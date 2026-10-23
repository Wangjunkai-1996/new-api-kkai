package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// The directory is provisioned on the shared persistent application volume.
// It has no static route; all file access remains behind invoice authorization.
type localInvoiceAssetStore struct {
	directory string
}

var invoicePrivateObjectKey = regexp.MustCompile(`^invoices/private/(0|[1-9][0-9]*)/[1-9][0-9]*/[0-9a-f]{48}$`)

func (s *localInvoiceAssetStore) openRoot() (*os.Root, error) {
	if !filepath.IsAbs(s.directory) || filepath.Clean(s.directory) != s.directory {
		return nil, ErrInvalidVideoAssetStoreRequest
	}
	resolved, err := filepath.EvalSymlinks(s.directory)
	if err != nil || resolved != s.directory {
		return nil, ErrInvalidVideoAssetStoreRequest
	}
	expected, err := os.Lstat(s.directory)
	if err != nil || !expected.IsDir() || expected.Mode().Perm() != 0700 {
		return nil, ErrInvalidVideoAssetStoreRequest
	}
	root, err := os.OpenRoot(s.directory)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, info) || !info.IsDir() || info.Mode().Perm() != 0700 {
		_ = root.Close()
		return nil, ErrInvalidVideoAssetStoreRequest
	}
	return root, nil
}

func invoiceLocalObjectName(key string) (string, error) {
	if !invoicePrivateObjectKey.MatchString(key) {
		return "", ErrInvalidVideoAssetStoreRequest
	}
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:]), nil
}

func (s *localInvoiceAssetStore) Get(ctx context.Context, key string) (VideoAssetObject, error) {
	if err := ctx.Err(); err != nil {
		return VideoAssetObject{}, err
	}
	name, err := invoiceLocalObjectName(key)
	if err != nil {
		return VideoAssetObject{}, err
	}
	root, err := s.openRoot()
	if err != nil {
		return VideoAssetObject{}, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return VideoAssetObject{}, ErrVideoAssetObjectNotFound
	}
	if err != nil {
		return VideoAssetObject{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return VideoAssetObject{}, ErrInvalidVideoAssetStoreRequest
	}
	file, err := root.Open(name)
	if err != nil {
		return VideoAssetObject{}, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0600 {
		_ = file.Close()
		return VideoAssetObject{}, ErrInvalidVideoAssetStoreRequest
	}
	return VideoAssetObject{Body: file, ContentLength: opened.Size()}, nil
}

func (s *localInvoiceAssetStore) Put(ctx context.Context, key, _ string, reader io.Reader, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil || size <= 0 || size > InvoiceAttachmentMaxBytes {
		return ErrInvalidVideoAssetStoreRequest
	}
	name, err := invoiceLocalObjectName(key)
	if err != nil {
		return err
	}
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if existing, err := root.Lstat(name); err == nil {
		if !existing.Mode().IsRegular() || existing.Mode().Perm() != 0600 {
			return ErrInvalidVideoAssetStoreRequest
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// A deterministic staging name lets the existing expiry worker remove bytes
	// left by a process crash as well as the completed object for this DB row.
	temporary := ".upload-" + name
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	defer file.Close()
	written, err := io.Copy(file, io.LimitReader(reader, size+1))
	if err != nil {
		return err
	}
	if written != size {
		return ErrInvalidVideoAssetStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := root.Rename(temporary, name); err != nil {
		return err
	}
	// Persist the rename before marking the database attachment as validated.
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (s *localInvoiceAssetStore) Delete(ctx context.Context, keys []string) error {
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		name, err := invoiceLocalObjectName(key)
		if err != nil {
			return err
		}
		for _, object := range []string{name, ".upload-" + name} {
			info, err := root.Lstat(object)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
				return ErrInvalidVideoAssetStoreRequest
			}
			if err := root.Remove(object); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	// Cleanup deletes the DB row after this returns; persist the unlinks first.
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
