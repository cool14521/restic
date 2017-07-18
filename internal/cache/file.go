package cache

import (
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	restic "github.com/restic/restic/internal"
	"github.com/restic/restic/internal/debug"
)

func (c *Cache) filename(h restic.Handle) string {
	if len(h.Name) < 2 {
		panic("Name is empty or too short")
	}
	subdir := h.Name[:2]
	return filepath.Join(c.Path, cacheLayoutPaths[h.Type], subdir, h.Name)
}

func (c *Cache) canBeCached(t restic.FileType) bool {
	if c == nil {
		return false
	}

	if _, ok := cacheLayoutPaths[t]; !ok {
		return false
	}

	return true
}

type readCloser struct {
	io.Reader
	io.Closer
}

// Load returns a reader that yields the contents of the file with the
// given handle. rd must be closed after use. If an error is returned, the
// ReadCloser is nil.
func (c *Cache) Load(h restic.Handle, length int, offset int64) (io.ReadCloser, error) {
	debug.Log("Load from cache: %v", h)
	if !c.canBeCached(h.Type) {
		return nil, errors.New("cannot be cached")
	}

	f, err := os.Open(c.filename(h))
	if err != nil {
		return nil, errors.Wrap(err, "Open")
	}

	if offset > 0 {
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
	}

	rd := readCloser{Reader: f, Closer: f}
	if length > 0 {
		rd.Reader = io.LimitReader(f, int64(length))
	}

	return rd, nil
}

// SaveWriter returns a writer for the cache object h. It must be closed after writing is finished.
func (c *Cache) SaveWriter(h restic.Handle) (io.WriteCloser, error) {
	debug.Log("Save to cache: %v", h)
	if !c.canBeCached(h.Type) {
		return nil, errors.New("cannot be cached")
	}

	p := c.filename(h)
	err := os.MkdirAll(filepath.Dir(p), 0700)
	if err != nil {
		return nil, errors.Wrap(err, "MkdirAll")
	}

	f, err := os.Create(p)
	if err != nil {
		return nil, errors.Wrap(err, "Create")
	}

	return f, err
}

// Save saves a file in the cache.
func (c *Cache) Save(h restic.Handle, rd io.Reader) error {
	debug.Log("Save to cache: %v", h)
	f, err := c.SaveWriter(h)
	if err != nil {
		return err
	}

	if _, err = io.Copy(f, rd); err != nil {
		f.Close()
		return errors.Wrap(err, "Copy")
	}

	if err = f.Close(); err != nil {
		return errors.Wrap(err, "Close")
	}

	return nil
}

// Remove deletes a file. When the file is not cache, no error is returned.
func (c *Cache) Remove(h restic.Handle) error {
	if !c.Has(h) {
		return nil
	}

	return os.Remove(c.filename(h))
}

// Clear removes all files of type t from the cache that are not contained in
// the set valid.
func (c *Cache) Clear(t restic.FileType, valid restic.IDSet) error {
	debug.Log("Clearing cache for %v: %v", t, valid)
	if !c.canBeCached(t) {
		return nil
	}

	list, err := c.list(t)
	if err != nil {
		return err
	}

	for id := range list {
		if valid.Has(id) {
			continue
		}

		if err = os.Remove(c.filename(restic.Handle{Type: t, Name: id.String()})); err != nil {
			return err
		}
	}

	return nil
}

func isFile(fi os.FileInfo) bool {
	return fi.Mode()&(os.ModeType|os.ModeCharDevice) == 0
}

// list returns a list of all files of type T in the cache.
func (c *Cache) list(t restic.FileType) (restic.IDSet, error) {
	if !c.canBeCached(t) {
		return nil, errors.New("cannot be cached")
	}

	list := restic.NewIDSet()
	dir := filepath.Join(c.Path, cacheLayoutPaths[t])
	err := filepath.Walk(dir, func(name string, fi os.FileInfo, err error) error {
		if err != nil {
			return errors.Wrap(err, "Walk")
		}

		if !isFile(fi) {
			return nil
		}

		id, err := restic.ParseID(filepath.Base(name))
		if err != nil {
			return nil
		}

		list.Insert(id)
		return nil
	})

	return list, err
}

// Has returns true if the file is cached.
func (c *Cache) Has(h restic.Handle) bool {
	if !c.canBeCached(h.Type) {
		return false
	}

	_, err := os.Stat(c.filename(h))
	if err == nil {
		return true
	}

	return false
}