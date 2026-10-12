package migration

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ArchiveVolume supports quiesced regular-file/directory volumes only. Symlinks,
// devices, sockets and special permission bits need explicit compatibility
// handling and are rejected rather than silently producing an incomplete copy.
func ArchiveVolume(ctx context.Context, source string, output io.Writer, writersStopped bool) error {
	if !writersStopped {
		return errors.New("volume writers must be stopped before backup")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	archive := tar.NewWriter(output)
	count := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 1000000 {
			return errors.New("volume exceeds entry limit")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if (!info.IsDir() && !info.Mode().IsRegular()) || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return errors.New("unsupported volume file type or permissions")
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(info, opened) || opened.Size() != info.Size() || !opened.Mode().IsRegular() {
			return errors.New("volume changed during backup")
		}
		if _, err := copyContext(ctx, archive, io.LimitReader(file, info.Size()), info.Size()); err != nil {
			return err
		}
		after, err := file.Stat()
		if err != nil {
			return err
		}
		if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return errors.New("volume changed during backup")
		}
		return nil
	})
	if err != nil {
		return err
	}
	return archive.Close()
}

// ExtractVolume requires a newly allocated private directory. It never replaces
// an existing file. The executor must discard or inspect the isolated directory
// after failure and may attach it to a container only after successful completion.
// maxBytes limits expanded contents independently of compressed/archive size.
func ExtractVolume(ctx context.Context, archive io.Reader, destination string, maxBytes int64) error {
	if maxBytes < 0 {
		return errors.New("invalid volume size limit")
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	reader := tar.NewReader(archive)
	seen := map[string]bool{}
	var directories []*tar.Header
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || strings.Contains(name, "\\") || strings.Contains(name, ":") || path.IsAbs(name) || path.Clean(name) != name || (name != "." && !filepath.IsLocal(name)) || seen[name] || len(seen) >= 1000000 {
			return errors.New("invalid or duplicate volume path")
		}
		seen[name] = true
		if header.Mode < 0 || header.Mode & ^int64(0777) != 0 || header.Uid < 0 || header.Gid < 0 {
			return errors.New("unsupported volume ownership or permissions")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if name != "." {
				if err := root.Mkdir(name, 0700); err != nil {
					return err
				}
			}
			copy := *header
			copy.Name = name
			directories = append(directories, &copy)
		case tar.TypeReg:
			if name == "." || header.Size < 0 || header.Size > maxBytes-total {
				return errors.New("volume exceeds size limit")
			}
			file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			_, err = copyContext(ctx, file, reader, header.Size)
			if err == nil {
				err = file.Chown(header.Uid, header.Gid)
			}
			if err == nil {
				err = file.Chmod(os.FileMode(header.Mode))
			}
			if err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			total += header.Size
		default:
			return errors.New("unsupported volume archive entry")
		}
	}
	// Apply restrictive directory permissions last, deepest first.
	for i := len(directories) - 1; i >= 0; i-- {
		header := directories[i]
		if err := root.Chown(header.Name, header.Uid, header.Gid); err != nil {
			return err
		}
		if err := root.Chmod(header.Name, os.FileMode(header.Mode)); err != nil {
			return err
		}
	}
	return nil
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader, expected int64) (int64, error) {
	buf := make([]byte, chunkSize)
	var count int64
	for count < expected {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		n, err := io.ReadFull(src, buf[:min(int64(len(buf)), expected-count)])
		if n > 0 {
			if writeErr := writeFull(dst, buf[:n]); writeErr != nil {
				return count, writeErr
			}
			count += int64(n)
		}
		if err != nil {
			return count, err
		}
	}
	return count, nil
}
