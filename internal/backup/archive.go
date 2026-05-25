package backup

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// writeArchive creates the ZIP file (or writes to stdout) and returns
// the report.
func writeArchive(opts *Options, tmpDir string, mf *manifest) (*Report, error) {
	outPath := resolveOutPath(opts)

	zw, outFile, err := openZipWriter(outPath)
	if err != nil {
		return nil, err
	}
	defer closeZipWriter(zw, outFile)

	// Snapshot DBs are already in tmpDir; add them first.
	for _, fe := range mf.Files {
		if strings.HasPrefix(fe.Path, "db/") {
			src := filepath.Join(tmpDir, filepath.Base(fe.Path))
			if err := addFileToZip(zw, src, fe.Path, mf); err != nil {
				return nil, fmt.Errorf("add %s: %w", fe.Path, err)
			}
		}
	}

	// Collect asset directories.
	if err := collectAssetDirs(zw, opts, mf); err != nil {
		return nil, err
	}

	// Write manifest.json last so it can reference all file hashes.
	if err := writeManifest(zw, mf, opts); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("finalize zip: %w", err)
	}

	return finalizeReport(outPath, outFile, opts)
}

func resolveOutPath(opts *Options) string {
	if opts.OutPath != "" {
		return opts.OutPath
	}
	t := opts.Now
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return fmt.Sprintf("backup-%s.zip", t.Format("2006-01-02-150405"))
}

func openZipWriter(outPath string) (*zip.Writer, *os.File, error) {
	if outPath == "-" {
		return zip.NewWriter(os.Stdout), nil, nil
	}
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("create zip: %w", err)
	}
	return zip.NewWriter(f), f, nil
}

func closeZipWriter(zw *zip.Writer, outFile *os.File) {
	if zw != nil {
		_ = zw.Close()
	}
	if outFile != nil {
		_ = outFile.Close()
	}
}

func collectAssetDirs(zw *zip.Writer, opts *Options, mf *manifest) error {
	type dirSpec struct {
		archivePrefix string
		src           string
		excludeName   string
		requiredFlag  bool
	}
	specs := []dirSpec{
		{"img", opts.ImageDir, "images", false},
		{"templates", opts.TemplateDir, "templates", false},
	}
	if opts.IncludePublic {
		specs = append(specs, dirSpec{"public", opts.RebuildOutDir, "", true})
	}

	stepNum := 2
	for _, spec := range specs {
		if spec.src == "" {
			continue
		}
		if isExcluded(opts.Excluded, spec.excludeName) {
			continue
		}
		if _, err := os.Stat(spec.src); os.IsNotExist(err) {
			// --include-public but dir missing: no-op, skip silently.
			continue
		}

		logStep(opts, "[%d/4] collecting %s/...", stepNum, spec.archivePrefix)
		stepNum++

		n, sz, err := collectDir(zw, spec.src, spec.archivePrefix, mf)
		if err != nil {
			return fmt.Errorf("collect %s: %w", spec.archivePrefix, err)
		}
		logStep(opts, " (%d files, %s)\n", n, humanSize(sz))
	}
	return nil
}

func writeManifest(zw *zip.Writer, mf *manifest, opts *Options) error {
	manifestBytes, err := mf.toJSON()
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	header := &zip.FileHeader{
		Name:     "manifest.json",
		Method:   zip.Deflate,
		Modified: opts.Now,
	}
	if header.Modified.IsZero() {
		header.Modified = time.Now()
	}
	w, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create manifest header: %w", err)
	}
	if _, err := w.Write(manifestBytes); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func finalizeReport(outPath string, outFile *os.File, opts *Options) (*Report, error) {
	var size int64
	if outFile != nil {
		if err := outFile.Sync(); err != nil {
			return nil, fmt.Errorf("sync zip: %w", err)
		}
		info, err := os.Stat(outPath)
		if err != nil {
			return nil, fmt.Errorf("stat zip: %w", err)
		}
		size = info.Size()
		_ = outFile.Close()
	}
	logStep(opts, "backup written: %s (%s)\n", outPath, humanSize(size))
	return &Report{OutPath: outPath, Size: size}, nil
}

// collectDir walks srcDir using os.Root and adds every file to the ZIP
// under archivePrefix. Returns the number of files and total bytes.
func collectDir(zw *zip.Writer, srcDir, archivePrefix string, mf *manifest) (int, int64, error) {
	root, err := os.OpenRoot(srcDir)
	if err != nil {
		return 0, 0, fmt.Errorf("open root %s: %w", srcDir, err)
	}
	defer func() { _ = root.Close() }()

	var count int
	var total int64

	walk := func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("non-local path: %s", rel)
		}

		f, err := root.Open(rel)
		if err != nil {
			return fmt.Errorf("open %s: %w", rel, err)
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}

		zipPath := filepath.Join(archivePrefix, filepath.ToSlash(rel))
		header := &zip.FileHeader{
			Name:     zipPath,
			Method:   zip.Deflate,
			Modified: info.ModTime(),
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create header %s: %w", zipPath, err)
		}

		hash := sha256.New()
		if _, err := io.Copy(io.MultiWriter(w, hash), f); err != nil {
			return fmt.Errorf("copy %s: %w", zipPath, err)
		}

		mf.addFile(zipPath, info.Size())
		mf.setSHA256(zipPath, hash.Sum(nil))
		count++
		total += info.Size()
		return nil
	}

	if err := walkRoot(root, ".", walk); err != nil {
		return 0, 0, err
	}
	return count, total, nil
}

func addFileToZip(zw *zip.Writer, srcPath, zipPath string, mf *manifest) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	header := &zip.FileHeader{
		Name:     zipPath,
		Method:   zip.Deflate,
		Modified: info.ModTime(),
	}
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, hash), f); err != nil {
		return err
	}
	mf.setSHA256(zipPath, hash.Sum(nil))
	return nil
}

// walkRoot recursively walks an os.Root, calling fn for every entry.
func walkRoot(root *os.Root, dir string, fn func(rel string, d fs.DirEntry, err error) error) error {
	return fs.WalkDir(root.FS(), dir, fn)
}

func isExcluded(list []string, name string) bool {
	for _, v := range list {
		if v == name {
			return true
		}
	}
	return false
}
