package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const previousUpdateBackups = 2

var updateBackupName = regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}Z\.before-v\d+\.\d+\.\d+\.db$`)

func (d *DB) CreateUpdateBackup(tag string) (string, error) {
	path, err := d.Path()
	if err != nil || path == "" {
		return "", err
	}
	dir := path + ".update-backups"
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + ".before-" + tag + ".db"
	if !updateBackupName.MatchString(name) {
		return "", fmt.Errorf("invalid update backup name %q", name)
	}
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	if err := checkUpdateBackupDir(dir); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, name)
	return dest, d.Backup(dest)
}

func (d *DB) ConfirmUpdateBackup(backup string) (bool, error) {
	root, name, err := d.openUpdateBackup(backup)
	if err != nil || root == nil {
		return false, err
	}
	defer root.Close()

	marker, err := root.OpenFile(name+".confirmed", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, fs.ErrExist) {
		return regularUpdateBackupFile(root, name+".confirmed")
	}
	if err != nil {
		return false, err
	}
	return true, marker.Close()
}

func (d *DB) PruneUpdateBackups(backup string) error {
	root, current, err := d.openUpdateBackup(backup)
	if err != nil || root == nil {
		return err
	}
	defer root.Close()
	if ok, err := regularUpdateBackupFile(root, current+".confirmed"); err != nil || !ok {
		return err
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	kept := 0
	for i := len(entries) - 1; i >= 0; i-- {
		name := entries[i].Name()
		if name >= current || !updateBackupName.MatchString(name) || !entries[i].Type().IsRegular() {
			continue
		}
		confirmed, err := regularUpdateBackupFile(root, name+".confirmed")
		if err != nil {
			return err
		}
		if confirmed && kept < previousUpdateBackups {
			kept++
			continue
		}
		if confirmed {
			if err := root.Remove(name + ".confirmed"); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (d *DB) openUpdateBackup(backup string) (*os.Root, string, error) {
	path, err := d.Path()
	if err != nil || path == "" {
		return nil, "", err
	}
	dir := path + ".update-backups"
	name := filepath.Base(backup)
	if !updateBackupName.MatchString(name) || backup != filepath.Join(dir, name) {
		return nil, "", nil
	}
	if err := checkUpdateBackupDir(dir); err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", err
	}
	if ok, err := regularUpdateBackupFile(root, name); err != nil || !ok {
		root.Close()
		return nil, "", err
	}
	return root, name, nil
}

func checkUpdateBackupDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a backup directory", dir)
	}
	return nil
}

func regularUpdateBackupFile(root *os.Root, name string) (bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil && info.Mode().IsRegular(), err
}
