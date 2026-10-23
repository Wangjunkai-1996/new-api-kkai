package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const invoiceLocalTestKey = "invoices/private/0/1/0123456789abcdef0123456789abcdef0123456789abcdef"

func TestInvoiceLocalStoreSharedPersistenceAndAtomicWrite(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(directory, 0700))
	t.Setenv("KKAI_INVOICE_STORAGE_DIR", directory)
	t.Setenv("KKAI_INVOICE_S3_PRIVATE", "false")
	first, err := InvoiceAssetStoreFromEnvironment(context.Background())
	require.NoError(t, err, "local private storage does not need S3 credentials")
	ctx := context.Background()
	content := []byte("private invoice")
	require.NoError(t, first.Put(ctx, invoiceLocalTestKey, "application/pdf", bytes.NewReader(content), int64(len(content))))
	second, err := InvoiceAssetStoreFromEnvironment(ctx)
	require.NoError(t, err)
	object, err := second.Get(ctx, invoiceLocalTestKey)
	require.NoError(t, err, "another slot/process must see persisted bytes")
	got, err := io.ReadAll(object.Body)
	require.NoError(t, err)
	require.NoError(t, object.Body.Close())
	assert.Equal(t, content, got)
	assert.EqualValues(t, len(content), object.ContentLength)

	for _, replacement := range []string{"short", "larger than declared"} {
		err := second.Put(ctx, invoiceLocalTestKey, "application/pdf", strings.NewReader(replacement), 10)
		require.Error(t, err, "incomplete/oversized transfer must not replace a validated file")
	}
	object, err = second.Get(ctx, invoiceLocalTestKey)
	require.NoError(t, err)
	got, err = io.ReadAll(object.Body)
	require.NoError(t, err)
	require.NoError(t, object.Body.Close())
	assert.Equal(t, content, got)
	files, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, files, 1, "failed writes must remove staging files")
	info, err := files[0].Info()
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	require.NoError(t, second.Delete(ctx, []string{invoiceLocalTestKey, invoiceLocalTestKey}))
	_, err = first.Get(ctx, invoiceLocalTestKey)
	assert.ErrorIs(t, err, ErrVideoAssetObjectNotFound)
	name, err := invoiceLocalObjectName(invoiceLocalTestKey)
	require.NoError(t, err)
	stagingPath := filepath.Join(directory, ".upload-"+name)
	require.NoError(t, os.WriteFile(stagingPath, []byte("partial crash transfer"), 0600))
	require.NoError(t, second.Delete(ctx, []string{invoiceLocalTestKey}), "expiry cleanup must remove interrupted transfers")
	_, err = os.Stat(stagingPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestInvoiceLocalStoreRejectsTraversalAndSymlinks(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(directory, 0700))
	store := &localInvoiceAssetStore{directory: directory}
	ctx := context.Background()
	for _, key := range []string{"../secret", "/etc/passwd", "invoices/private/0/1/../../secret", "invoices/private/0/1/" + strings.Repeat("a", 48) + "/other"} {
		_, err := store.Get(ctx, key)
		assert.Error(t, err)
		assert.Error(t, store.Put(ctx, key, "image/png", strings.NewReader("x"), 1))
		assert.Error(t, store.Delete(ctx, []string{key}))
	}
	outside := filepath.Join(t.TempDir(), "private")
	require.NoError(t, os.WriteFile(outside, []byte("do not expose or change"), 0600))
	name, err := invoiceLocalObjectName(invoiceLocalTestKey)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(outside, filepath.Join(directory, name)))
	_, err = store.Get(ctx, invoiceLocalTestKey)
	assert.Error(t, err)
	assert.Error(t, store.Put(ctx, invoiceLocalTestKey, "image/png", strings.NewReader("x"), 1))
	assert.Error(t, store.Delete(ctx, []string{invoiceLocalTestKey}))
	got, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "do not expose or change", string(got))

	link := filepath.Join(t.TempDir(), "invoice-link")
	require.NoError(t, os.Symlink(directory, link))
	t.Setenv("KKAI_INVOICE_STORAGE_DIR", link)
	_, err = InvoiceAssetStoreFromEnvironment(ctx)
	invoiceRequireCode(t, err, "STORAGE_UNAVAILABLE")
	require.NoError(t, os.Chmod(directory, 0755))
	t.Setenv("KKAI_INVOICE_STORAGE_DIR", directory)
	_, err = InvoiceAssetStoreFromEnvironment(ctx)
	invoiceRequireCode(t, err, "STORAGE_UNAVAILABLE")
}

func TestInvoiceAssetsLocalStoragePreservesAuthorizedDelivery(t *testing.T) {
	s, content, owner, admin := invoiceAssetFixture(t)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(directory, 0700))
	s.Store = &localInvoiceAssetStore{directory: directory}
	attachment, err := s.Upload(context.Background(), admin, "owned-request", "local-delivery", "invoice", "invoice.png", bytes.NewReader(content))
	require.NoError(t, err)
	_, _, err = s.Open(context.Background(), owner, "owned-request", attachment.ID)
	require.Error(t, err, "users must not read an administrator's unissued draft")
	_, object, err := s.Open(context.Background(), admin, "owned-request", attachment.ID)
	require.NoError(t, err)
	got, err := io.ReadAll(object.Body)
	require.NoError(t, err)
	require.NoError(t, object.Body.Close())
	assert.Equal(t, content, got)
}
