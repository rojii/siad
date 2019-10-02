package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/NebulousLabs/Sia/modules"

	"gitlab.com/NebulousLabs/Sia/build"
)

// testDir creates a testing directory for a filesystem test.
func testDir(name string) string {
	dir := build.TempDir(name, filepath.Join("filesystem"))
	if err := os.MkdirAll(dir, 0777); err != nil {
		panic(err)
	}
	return dir
}

// newSiaPath creates a new siapath from the specified string.
func newSiaPath(path string) modules.SiaPath {
	sp, err := modules.NewSiaPath(path)
	if err != nil {
		panic(err)
	}
	return sp
}

// newTestFileSystem creates a new filesystem for testing.
func newTestFileSystem(root string) *FileSystem {
	fs, err := New(root)
	if err != nil {
		panic(err.Error())
	}
	return fs
}

// TestNew tests creating a new FileSystem.
func TestNew(t *testing.T) {
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Check fields.
	if fs.staticParent != nil {
		t.Fatalf("fs.parent shoud be 'nil' but wasn't")
	}
	if fs.staticName != root {
		t.Fatalf("fs.staticName should be %v but was %v", root, fs.staticName)
	}
	if fs.threads == nil || len(fs.threads) != 0 {
		t.Fatal("fs.threads is not an empty initialized map")
	}
	if fs.threadUID != 0 {
		t.Fatalf("fs.threadUID should be 0 but was %v", fs.threadUID)
	}
	if fs.directories == nil || len(fs.directories) != 0 {
		t.Fatal("fs.directories is not an empty initialized map")
	}
	if fs.files == nil || len(fs.files) != 0 {
		t.Fatal("fs.files is not an empty initialized map")
	}
	// Create the filesystem again at the same location.
	_ = newTestFileSystem(fs.staticName)
}

// TestNewSiaDir tests if creating a new directory using NewSiaDir creates the
// correct folder structure.
func TestNewSiaDir(t *testing.T) {
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create dir /sub/foo
	sp := newSiaPath("sub/foo")
	if err := fs.NewSiaDir(sp); err != nil {
		t.Fatal(err)
	}
	// The whole path should exist.
	if _, err := os.Stat(filepath.Join(root, sp.String())); err != nil {
		t.Fatal(err)
	}
}

// TestOpenSiaDir confirms that a previoiusly created SiaDir can be opened and
// that the filesystem tree is extended accordingly in the process.
func TestOpenSiaDir(t *testing.T) {
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create dir /sub/foo
	sp := newSiaPath("sub/foo")
	if err := fs.NewSiaDir(sp); err != nil {
		t.Fatal(err)
	}
	// Open the newly created dir.
	sd, err := fs.OpenSiaDir(sp)
	if err != nil {
		t.Fatal(err)
	}
	defer sd.close()
	// Confirm the integrity of the root node.
	if len(fs.threads) != 0 {
		t.Fatalf("Expected fs.threads to have length 0 but was %v", len(fs.threads))
	}
	if len(fs.directories) != 1 {
		t.Fatalf("Expected 1 subdirectory in the root but got %v", len(fs.directories))
	}
	if len(fs.files) != 0 {
		t.Fatalf("Expected 0 files in the root but got %v", len(fs.files))
	}
	// Confirm the integrity of the /sub node.
	subNode, exists := fs.directories["sub"]
	if !exists {
		t.Fatal("expected root to contain the 'sub' node")
	}
	if subNode.staticName != "sub" {
		t.Fatalf("subNode name should be 'sub' but was %v", subNode.staticName)
	}
	if len(subNode.threads) != 0 {
		t.Fatalf("expected 0 threads in subNode but got %v", len(subNode.threads))
	}
	if len(subNode.directories) != 1 {
		t.Fatalf("Expected 1 subdirectory in the root but got %v", len(subNode.directories))
	}
	if len(subNode.files) != 0 {
		t.Fatalf("Expected 0 files in the root but got %v", len(subNode.files))
	}
	// Confirm the integrity of the /sub/foo node.
	fooNode, exists := subNode.directories["foo"]
	if !exists {
		t.Fatal("expected /sub to contain /sub/foo")
	}
	if fooNode.staticName != "foo" {
		t.Fatalf("fooNode name should be 'foo' but was %v", fooNode.staticName)
	}
	if len(fooNode.threads) != 1 {
		t.Fatalf("expected 1 thread in fooNode but got %v", len(fooNode.threads))
	}
	if len(fooNode.directories) != 0 {
		t.Fatalf("Expected 0 subdirectory in the fooNode but got %v", len(fooNode.directories))
	}
	if len(fooNode.files) != 0 {
		t.Fatalf("Expected 0 files in the root but got %v", len(fooNode.files))
	}
	// Open the newly created dir again.
	sd2, err := fs.OpenSiaDir(sp)
	if err != nil {
		t.Fatal(err)
	}
	defer sd2.close()
	// They should have different UIDs.
	if sd.threadUID == 0 {
		t.Fatal("threaduid shouldn't be 0")
	}
	if sd2.threadUID == 0 {
		t.Fatal("threaduid shouldn't be 0")
	}
	if sd.threadUID == sd2.threadUID {
		t.Fatal("sd and sd2 should have different threaduids")
	}
	if len(sd.threads) != 2 || len(sd2.threads) != 2 {
		t.Fatal("sd and sd2 should both have 2 threads registered")
	}
	_, exists1 := sd.threads[sd.threadUID]
	_, exists2 := sd.threads[sd2.threadUID]
	_, exists3 := sd2.threads[sd.threadUID]
	_, exists4 := sd2.threads[sd2.threadUID]
	if exists := exists1 && exists2 && exists3 && exists4; !exists {
		t.Fatal("sd and sd1's threads don't contain the right uids")
	}
	// Open /sub manually and make sure that subDir and sdSub are consistent.
	sdSub, err := fs.OpenSiaDir(newSiaPath("sub"))
	if err != nil {
		t.Fatal(err)
	}
	defer sdSub.close()
	if len(subNode.threads) != 1 || len(sdSub.threads) != 1 {
		t.Fatal("subNode and sdSub should both have 1 thread registered")
	}
	if len(subNode.directories) != 1 || len(sdSub.directories) != 1 {
		t.Fatal("subNode and sdSub should both have 1 subdir")
	}
	if len(subNode.files) != 0 || len(sdSub.files) != 0 {
		t.Fatal("subNode and sdSub should both have 0 files")
	}
}

// TestCloseSiaDir tests that closing an opened directory shrings the tree
// accordingly.
func TestCloseSiaDir(t *testing.T) {
	t.Fatal("not implemented yet")
}
