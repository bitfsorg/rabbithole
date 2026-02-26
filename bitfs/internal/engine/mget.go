package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MgetOpts holds options for the Mget (recursive download) operation.
type MgetOpts struct {
	VaultIndex uint32
	RemotePath string // remote directory to download
	LocalDir   string // local directory to download into
}

// MgetResult summarizes a recursive download.
type MgetResult struct {
	DirsCreated     int
	FilesDownloaded int
	Errors          []string
}

// Mget recursively downloads a vault directory to local disk.
func (e *Engine) Mget(opts *MgetOpts) (*MgetResult, error) {
	// Resolve the remote directory.
	node := e.State.FindNodeByPath(opts.RemotePath)
	if node == nil {
		return nil, fmt.Errorf("engine: %q not found", opts.RemotePath)
	}
	if node.Type != "dir" {
		return nil, fmt.Errorf("engine: %q is not a directory", opts.RemotePath)
	}

	result := &MgetResult{}
	e.mgetRecurse(opts.VaultIndex, node, opts.LocalDir, result)
	return result, nil
}

// mgetRecurse recursively downloads directory contents.
func (e *Engine) mgetRecurse(vaultIdx uint32, dir *NodeState, localDir string, result *MgetResult) {
	// Create local directory.
	if err := os.MkdirAll(localDir, 0755); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("mkdir %s: %v", localDir, err))
		return
	}
	result.DirsCreated++

	for _, child := range dir.Children {
		// Validate child name to prevent path traversal from untrusted Metanet DAG state.
		if strings.Contains(child.Name, "..") || strings.ContainsAny(child.Name, "/\\") || child.Name == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("unsafe child name %q, skipping", child.Name))
			continue
		}

		childNode := e.State.GetNode(child.PubKey)
		if childNode == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("node %s not found for %s", child.PubKey[:8], child.Name))
			continue
		}

		switch childNode.Type {
		case "dir":
			childLocalDir := filepath.Join(localDir, child.Name)
			e.mgetRecurse(vaultIdx, childNode, childLocalDir, result)
		case "file":
			localPath := filepath.Join(localDir, child.Name)
			_, getErr := e.Get(&GetOpts{
				VaultIndex: vaultIdx,
				RemotePath: childNode.Path,
				LocalPath:  localPath,
			})
			if getErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("get %s: %v", childNode.Path, getErr))
			} else {
				result.FilesDownloaded++
			}
		}
	}
}
