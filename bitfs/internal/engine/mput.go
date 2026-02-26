package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MputOpts holds options for the Mput (recursive upload) operation.
type MputOpts struct {
	VaultIndex uint32
	LocalDir   string // local directory to upload
	RemoteDir  string // remote directory base (default: cwd)
	Access     string // "free" or "private", applied to all files
}

// MputResult summarizes a recursive upload.
type MputResult struct {
	DirsCreated   int
	FilesUploaded int
	Errors        []string
}

// Mput recursively uploads a local directory to the vault.
func (e *Engine) Mput(opts *MputOpts) (*MputResult, error) {
	// Validate remote directory path.
	if opts.RemoteDir == "" {
		return nil, fmt.Errorf("mput: remote directory path cannot be empty")
	}
	if strings.Contains(opts.RemoteDir, "..") {
		return nil, fmt.Errorf("mput: remote directory path must not contain '..' components")
	}

	// Verify local directory exists.
	info, err := os.Stat(opts.LocalDir)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("engine: %q is not a directory", opts.LocalDir)
	}

	access := opts.Access
	if access == "" {
		access = "free"
	}

	result := &MputResult{}
	baseDir := filepath.Clean(opts.LocalDir)

	err = filepath.WalkDir(baseDir, func(localPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("walk %s: %v", localPath, walkErr))
			return nil // skip this entry, keep walking
		}

		// Explicitly skip symlinks and special files (defense in depth).
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		// Compute relative path from baseDir.
		rel, err := filepath.Rel(baseDir, localPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("rel %s: %v", localPath, err))
			return nil
		}

		// Skip the base directory itself.
		if rel == "." {
			return nil
		}

		// Build remote path: remoteDir + "/" + relative path (using forward slashes).
		remotePath := opts.RemoteDir
		if !strings.HasSuffix(remotePath, "/") {
			remotePath += "/"
		}
		remotePath += filepath.ToSlash(rel)

		if d.IsDir() {
			_, mkErr := e.Mkdir(&MkdirOpts{
				VaultIndex: opts.VaultIndex,
				Path:       remotePath,
			})
			if mkErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("mkdir %s: %v", remotePath, mkErr))
			} else {
				result.DirsCreated++
			}
		} else if d.Type().IsRegular() {
			_, putErr := e.PutFile(&PutOpts{
				VaultIndex: opts.VaultIndex,
				LocalFile:  localPath,
				RemotePath: remotePath,
				Access:     access,
			})
			if putErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("put %s: %v", remotePath, putErr))
			} else {
				result.FilesUploaded++
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("engine: walk directory: %w", err)
	}

	return result, nil
}
