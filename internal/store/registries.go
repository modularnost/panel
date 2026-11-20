package store

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

type Registry struct {
	Host     string
	Username string
}

func RegistryCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("registry encryption requires a 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func (d *DB) Registries() ([]Registry, error) {
	rows, err := d.Query(`SELECT host, username FROM registries ORDER BY host`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Registry{}
	for rows.Next() {
		var r Registry
		if err := rows.Scan(&r.Host, &r.Username); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) SaveRegistry(r Registry, token string, encryption cipher.AEAD) error {
	sealed := encryption.Seal(nil, nil, []byte(token), []byte(r.Host+"\x00"+r.Username))
	_, err := d.Exec(`INSERT INTO registries (host, username, encrypted_token) VALUES (?, ?, ?)
		ON CONFLICT(host) DO UPDATE SET username=excluded.username, encrypted_token=excluded.encrypted_token`,
		r.Host, r.Username, sealed)
	return err
}

func (d *DB) RegistryCredentials(host string, encryption cipher.AEAD) (string, string, error) {
	var username string
	var sealed []byte
	if err := d.QueryRow(`SELECT username, encrypted_token FROM registries WHERE host=?`, host).Scan(&username, &sealed); err != nil {
		return "", "", err
	}
	token, err := encryption.Open(nil, nil, sealed, []byte(host+"\x00"+username))
	if err != nil {
		return "", "", err
	}
	return username, string(token), nil
}

func (d *DB) DeleteRegistry(host string) error {
	_, err := d.Exec(`DELETE FROM registries WHERE host=?`, host)
	return err
}
