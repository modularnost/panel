package store

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Roles, weakest first. Three is all that is needed for now (see the ADR).
const (
	RoleViewer   = "viewer"   // read services, logs and history
	RoleOperator = "operator" // + update services and stacks
	RoleAdmin    = "admin"    // + webhooks, Traefik labels, users, tokens
)

var rank = map[string]int{RoleViewer: 1, RoleOperator: 2, RoleAdmin: 3}

// AtLeast reports whether role have satisfies the required role need.
func AtLeast(have, need string) bool { return rank[need] > 0 && rank[have] >= rank[need] }

func ValidRole(r string) bool { return rank[r] > 0 }

// --- Passwords ---

// ponytail: PBKDF2-SHA256 from the stdlib instead of a bcrypt/argon2 dependency.
// The iteration count follows the OWASP recommendation; raise it here when that
// changes — old hashes stay valid because the count lives in the hash string.
const pbkdf2Iter = 600_000

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iter, 32)
	if err != nil {
		return "", err
	}
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter, b64(salt), b64(key)), nil
}

func CheckPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// --- Users ---

type User struct {
	ID       int64
	Username string
	Role     string
}

var ErrNoUser = errors.New("wrong username or password")

func (d *DB) CreateUser(username, password, role string) (User, error) {
	if !ValidRole(role) {
		return User{}, fmt.Errorf("unknown role %q", role)
	}
	if username == "" {
		return User{}, errors.New("empty username")
	}
	if len(password) < 8 {
		return User{}, errors.New("password shorter than 8 characters")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	res, err := d.Exec(`INSERT INTO users (username, password_hash, role) VALUES (?,?,?)`,
		username, hash, role)
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Username: username, Role: role}, nil
}

func (d *DB) SetPassword(id int64, password string) error {
	if len(password) < 8 {
		return errors.New("password shorter than 8 characters")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (d *DB) DeleteUser(id int64) error {
	_, err := d.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (d *DB) Users() ([]User, error) {
	rows, err := d.Query(`SELECT id, username, role FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (d *DB) CountUsers() (int, error) {
	var n int
	return n, d.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
}

// IsLastAdmin reports whether this user is the only admin left. It exists so
// the last one cannot be deleted or demoted.
func (d *DB) IsLastAdmin(id int64) (bool, error) {
	var admins int
	var role string
	err := d.QueryRow(`SELECT (SELECT count(*) FROM users WHERE role='admin'),
		coalesce((SELECT role FROM users WHERE id=?), '')`, id).Scan(&admins, &role)
	return role == RoleAdmin && admins < 2, err
}

func (d *DB) SetRole(id int64, role string) error {
	if !ValidRole(role) {
		return fmt.Errorf("unknown role %q", role)
	}
	_, err := d.Exec(`UPDATE users SET role=? WHERE id=?`, role, id)
	return err
}

// Authenticate checks a username and password.
func (d *DB) Authenticate(username, password string) (User, error) {
	var u User
	var hash string
	err := d.QueryRow(`SELECT id, username, role, password_hash FROM users WHERE username=?`,
		username).Scan(&u.ID, &u.Username, &u.Role, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Hash even for a missing user, otherwise response timing leaks which
		// usernames exist.
		CheckPassword(dummyHash, password)
		return User{}, ErrNoUser
	}
	if err != nil {
		return User{}, err
	}
	if !CheckPassword(hash, password) {
		return User{}, ErrNoUser
	}
	return u, nil
}

// dummyHash never matches but costs the same to verify.
var dummyHash = func() string {
	h, _ := HashPassword("-")
	return h
}()

// --- Tokens (browser sessions and CI keys) ---

type Token struct {
	ID        int64
	UserID    int64
	Username  string
	Name      string
	Role      string // the token's own role; empty means inherit the user's
	ExpiresAt sql.NullTime
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

// CreateToken returns the token itself — only its hash is stored, so there is
// nothing to show a second time.
func (d *DB) CreateToken(userID int64, name, role string, ttl time.Duration) (string, error) {
	if role != "" && !ValidRole(role) {
		return "", fmt.Errorf("unknown role %q", role)
	}
	token := NewToken()
	var expires any // nil = never expires
	if ttl != 0 {
		expires = time.Now().Add(ttl)
	}
	_, err := d.Exec(`INSERT INTO api_tokens (user_id, name, token_hash, role, expires_at) VALUES (?,?,?,?,?)`,
		userID, name, hashToken(token), role, expires)
	return token, err
}

// UserByToken returns the user carrying the token's effective role.
func (d *DB) UserByToken(token string) (User, error) {
	var u User
	var tokenRole string
	var expires sql.NullTime
	err := d.QueryRow(`SELECT u.id, u.username, u.role, t.role, t.expires_at
		FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ?`,
		hashToken(token)).Scan(&u.ID, &u.Username, &u.Role, &tokenRole, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNoUser
	}
	if err != nil {
		return User{}, err
	}
	if expires.Valid && expires.Time.Before(time.Now()) {
		return User{}, ErrNoUser
	}
	// A token may narrow the user's rights, never widen them.
	if tokenRole != "" && !AtLeast(tokenRole, u.Role) {
		u.Role = tokenRole
	}
	return u, nil
}

func (d *DB) DeleteToken(id int64) error {
	_, err := d.Exec(`DELETE FROM api_tokens WHERE id=?`, id)
	return err
}

func (d *DB) DeleteTokenByValue(token string) error {
	_, err := d.Exec(`DELETE FROM api_tokens WHERE token_hash=?`, hashToken(token))
	return err
}

// Tokens lists named tokens only — browser sessions stay out of this list.
func (d *DB) Tokens() ([]Token, error) {
	rows, err := d.Query(`SELECT t.id, t.user_id, u.username, t.name, t.role, t.expires_at
		FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.name != '' ORDER BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.UserID, &t.Username, &t.Name, &t.Role, &t.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PurgeExpiredTokens drops expired sessions; call it at startup.
func (d *DB) PurgeExpiredTokens() error {
	_, err := d.Exec(`DELETE FROM api_tokens WHERE expires_at IS NOT NULL AND expires_at < CURRENT_TIMESTAMP`)
	return err
}
