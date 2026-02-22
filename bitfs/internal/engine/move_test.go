package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupMoveTestEngine sets up a test engine with root, /src, /dst directories,
// and a file at /src/file.txt. Returns the engine. Multiple fee UTXOs are
// pre-loaded to support cross-directory moves (which need 2 txs).
func setupMoveTestEngine(t *testing.T) *Engine {
	t.Helper()
	eng := initTestEngine(t)

	// Add many fee UTXOs — cross-directory moves need several.
	for i := 0; i < 10; i++ {
		addFeeUTXO(t, eng, 100000)
	}

	// Create root directory.
	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/"})
	if err != nil {
		t.Fatalf("Mkdir /: %v", err)
	}

	// Create /src directory.
	_, err = eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/src"})
	if err != nil {
		t.Fatalf("Mkdir /src: %v", err)
	}

	// Create /dst directory.
	_, err = eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/dst"})
	if err != nil {
		t.Fatalf("Mkdir /dst: %v", err)
	}

	// Create a local test file and upload it to /src/file.txt.
	testFile := filepath.Join(eng.DataDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	_, err = eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  testFile,
		RemotePath: "/src/file.txt",
		Access:     "free",
	})
	if err != nil {
		t.Fatalf("PutFile /src/file.txt: %v", err)
	}

	return eng
}

func TestMove_SameDirectory(t *testing.T) {
	eng := setupMoveTestEngine(t)

	// Rename /src/file.txt to /src/renamed.txt (same directory).
	result, err := eng.Move(&MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/src/file.txt",
		DstPath:    "/src/renamed.txt",
	})
	if err != nil {
		t.Fatalf("Move same-dir: %v", err)
	}

	if result.TxHex == "" {
		t.Error("expected non-empty TxHex")
	}
	if result.TxID == "" {
		t.Error("expected non-empty TxID")
	}
	if !strings.Contains(result.Message, "Moved") {
		t.Errorf("message should contain 'Moved', got: %s", result.Message)
	}

	// Verify old path is gone and new path exists.
	old := eng.State.FindNodeByPath("/src/file.txt")
	if old != nil {
		t.Error("old path /src/file.txt should no longer resolve")
	}
	newNode := eng.State.FindNodeByPath("/src/renamed.txt")
	if newNode == nil {
		t.Error("new path /src/renamed.txt should exist")
	}

	// Verify the parent's children list reflects the rename.
	srcDir := eng.State.FindNodeByPath("/src")
	if srcDir == nil {
		t.Fatal("/src directory not found")
	}
	found := false
	for _, c := range srcDir.Children {
		if c.Name == "renamed.txt" {
			found = true
		}
		if c.Name == "file.txt" {
			t.Error("old name 'file.txt' should not be in /src children")
		}
	}
	if !found {
		t.Error("'renamed.txt' should be in /src children")
	}
}

func TestMove_CrossDirectory(t *testing.T) {
	eng := setupMoveTestEngine(t)

	// Get the node's pubkey before the move.
	srcNode := eng.State.FindNodeByPath("/src/file.txt")
	if srcNode == nil {
		t.Fatal("source node /src/file.txt not found")
	}
	originalPubKey := srcNode.PubKeyHex

	// Move /src/file.txt to /dst/file.txt (cross-directory).
	result, err := eng.Move(&MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/src/file.txt",
		DstPath:    "/dst/file.txt",
	})
	if err != nil {
		t.Fatalf("Move cross-dir: %v", err)
	}

	if result.TxHex == "" {
		t.Error("expected non-empty TxHex")
	}
	if result.TxID == "" {
		t.Error("expected non-empty TxID")
	}
	if !strings.Contains(result.Message, "2 txs") {
		t.Errorf("message should mention '2 txs', got: %s", result.Message)
	}

	// Verify the node's path has been updated.
	movedNode := eng.State.FindNodeByPath("/dst/file.txt")
	if movedNode == nil {
		t.Fatal("moved node /dst/file.txt should exist")
	}

	// The node's pubkey must be the same (move doesn't change identity).
	if movedNode.PubKeyHex != originalPubKey {
		t.Errorf("node pubkey changed: got %s, want %s", movedNode.PubKeyHex, originalPubKey)
	}

	// The old path should no longer resolve.
	oldNode := eng.State.FindNodeByPath("/src/file.txt")
	if oldNode != nil {
		t.Error("old path /src/file.txt should no longer resolve")
	}

	// Verify /src no longer lists file.txt.
	srcDir := eng.State.FindNodeByPath("/src")
	if srcDir == nil {
		t.Fatal("/src directory not found")
	}
	for _, c := range srcDir.Children {
		if c.Name == "file.txt" {
			t.Error("'file.txt' should have been removed from /src children")
		}
	}

	// Verify /dst now lists file.txt.
	dstDir := eng.State.FindNodeByPath("/dst")
	if dstDir == nil {
		t.Fatal("/dst directory not found")
	}
	found := false
	for _, c := range dstDir.Children {
		if c.Name == "file.txt" {
			found = true
			// Check that the pubkey is preserved.
			if c.PubKey != originalPubKey {
				t.Errorf("child pubkey changed: got %s, want %s", c.PubKey, originalPubKey)
			}
		}
	}
	if !found {
		t.Error("'file.txt' should be in /dst children")
	}
}

func TestMove_CrossDirectory_WithRename(t *testing.T) {
	eng := setupMoveTestEngine(t)

	// Move /src/file.txt to /dst/newname.txt (cross-directory + rename).
	result, err := eng.Move(&MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/src/file.txt",
		DstPath:    "/dst/newname.txt",
	})
	if err != nil {
		t.Fatalf("Move cross-dir with rename: %v", err)
	}

	if result.TxID == "" {
		t.Error("expected non-empty TxID")
	}

	// Verify the node exists at the new path with the new name.
	movedNode := eng.State.FindNodeByPath("/dst/newname.txt")
	if movedNode == nil {
		t.Fatal("moved node /dst/newname.txt should exist")
	}

	// Verify the child entry in /dst has the new name.
	dstDir := eng.State.FindNodeByPath("/dst")
	if dstDir == nil {
		t.Fatal("/dst directory not found")
	}
	found := false
	for _, c := range dstDir.Children {
		if c.Name == "newname.txt" {
			found = true
		}
	}
	if !found {
		t.Error("'newname.txt' should be in /dst children")
	}
}

func TestMove_CrossDirectory_SourceNodeNotFound(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Move(&MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/dir1/nonexistent",
		DstPath:    "/dir2/nonexistent",
	})
	if err == nil {
		t.Error("Move with missing source should fail")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestMove_CrossDirectory_DstDirNotFound(t *testing.T) {
	eng := setupMoveTestEngine(t)

	_, err := eng.Move(&MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/src/file.txt",
		DstPath:    "/nonexistent/file.txt",
	})
	if err == nil {
		t.Error("Move to nonexistent destination dir should fail")
	}
	if !strings.Contains(err.Error(), "destination directory") {
		t.Errorf("error should mention 'destination directory', got: %v", err)
	}
}

func TestMove_CrossDirectory_SrcDirNotFound(t *testing.T) {
	eng := initTestEngine(t)

	// Manually create a node at a path whose parent dir doesn't exist in state.
	eng.State.SetNode("fakepub", &NodeState{
		PubKeyHex:  "fakepub",
		Type:       "file",
		Path:       "/ghost/file.txt",
		VaultIndex: 0,
	})

	_, err := eng.Move(&MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/ghost/file.txt",
		DstPath:    "/dst/file.txt",
	})
	if err == nil {
		t.Error("Move from nonexistent source dir should fail")
	}
	if !strings.Contains(err.Error(), "source directory") {
		t.Errorf("error should mention 'source directory', got: %v", err)
	}
}

func TestMove_CrossDirectory_DuplicateDestName(t *testing.T) {
	eng := setupMoveTestEngine(t)

	// Create a file in /dst with the same name.
	testFile := filepath.Join(eng.DataDir, "conflict.txt")
	if err := os.WriteFile(testFile, []byte("conflict"), 0644); err != nil {
		t.Fatalf("write conflict file: %v", err)
	}
	_, err := eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  testFile,
		RemotePath: "/dst/file.txt",
		Access:     "free",
	})
	if err != nil {
		t.Fatalf("PutFile /dst/file.txt: %v", err)
	}

	// Try to move /src/file.txt to /dst/file.txt — should fail.
	_, err = eng.Move(&MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/src/file.txt",
		DstPath:    "/dst/file.txt",
	})
	if err == nil {
		t.Error("Move to existing destination should fail")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestMove_CrossDirectory_FromRoot(t *testing.T) {
	eng := initTestEngine(t)

	// Add many fee UTXOs.
	for i := 0; i < 10; i++ {
		addFeeUTXO(t, eng, 100000)
	}

	// Create root.
	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/"})
	if err != nil {
		t.Fatalf("Mkdir /: %v", err)
	}

	// Create /subdir.
	_, err = eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/subdir"})
	if err != nil {
		t.Fatalf("Mkdir /subdir: %v", err)
	}

	// Create a file at root level.
	testFile := filepath.Join(eng.DataDir, "root_file.txt")
	if err := os.WriteFile(testFile, []byte("root content"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	_, err = eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  testFile,
		RemotePath: "/root_file.txt",
		Access:     "free",
	})
	if err != nil {
		t.Fatalf("PutFile /root_file.txt: %v", err)
	}

	// Move from root to subdir.
	result, err := eng.Move(&MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/root_file.txt",
		DstPath:    "/subdir/moved_file.txt",
	})
	if err != nil {
		t.Fatalf("Move from root to subdir: %v", err)
	}
	if result.TxID == "" {
		t.Error("expected non-empty TxID")
	}

	// Verify the node moved.
	movedNode := eng.State.FindNodeByPath("/subdir/moved_file.txt")
	if movedNode == nil {
		t.Fatal("node should be at /subdir/moved_file.txt")
	}
	oldNode := eng.State.FindNodeByPath("/root_file.txt")
	if oldNode != nil {
		t.Error("old path should no longer resolve")
	}

	// Verify root no longer has the file.
	rootPubHex, _ := eng.getRootPubHex(0)
	rootNode := eng.State.GetNode(rootPubHex)
	for _, c := range rootNode.Children {
		if c.Name == "root_file.txt" {
			t.Error("'root_file.txt' should have been removed from root children")
		}
	}

	// Verify /subdir has the file.
	subdir := eng.State.FindNodeByPath("/subdir")
	found := false
	for _, c := range subdir.Children {
		if c.Name == "moved_file.txt" {
			found = true
		}
	}
	if !found {
		t.Error("'moved_file.txt' should be in /subdir children")
	}
}
