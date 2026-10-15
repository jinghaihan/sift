package store

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jinghaihan/sift/internal/review"
)

func concurrentScope() review.Scope {
	return review.Scope{Repository: "repo", Branch: "feature", Integration: "dev", Start: "start"}
}

func TestCommentJSONRoundTripDoesNotPreventDeletion(t *testing.T) {
	dir := t.TempDir()
	db, record, err := Open(dir, concurrentScope())
	if err != nil {
		t.Fatal(err)
	}
	// Exercise both a monotonic reading and an explicit non-UTC location.
	record.Comments = []Comment{
		{ID: "monotonic", Body: "first", Created: time.Now()},
		{ID: "local", Body: "second", Created: time.Now().In(time.FixedZone("test", 8*60*60))},
	}
	record.Sequence++
	if err = db.Save(record); err != nil {
		t.Fatal(err)
	}
	record.Comments = nil
	record.Sequence++
	if err = db.Save(record); err != nil {
		t.Fatalf("JSON timestamp conversion was mistaken for a concurrent edit: %v", err)
	}
	_, saved, err := Open(dir, concurrentScope())
	if err != nil || len(saved.Comments) != 0 {
		t.Fatalf("deleted comments returned after reopening: %+v %v", saved, err)
	}
}

func TestOldRevisionWriterCannotStampOldAnchorsAsCurrent(t *testing.T) {
	dir := t.TempDir()
	db, record, _ := Open(dir, concurrentScope())
	record.Tip, record.Sequence = "old", 1
	if err := db.Save(record); err != nil {
		t.Fatal(err)
	}
	a, old, _ := Open(dir, concurrentScope())
	b, newer, _ := Open(dir, concurrentScope())
	newer.Tip, newer.Sequence = "new", 2
	if err := b.Save(newer); err != nil {
		t.Fatal(err)
	}
	old.Sequence++
	old.Comments = []Comment{{ID: "note", Body: "Anchored on old code."}}
	if err := a.Save(old); !errors.Is(err, ErrConcurrentWrite) {
		t.Fatalf("old anchors were silently associated with new code: %v", err)
	}
}

func TestIndependentWritersMergeAdditionsAndRejectConflictingEdits(t *testing.T) {
	dir := t.TempDir()
	scope := concurrentScope()
	a, ra, _ := Open(dir, scope)
	b, rb, _ := Open(dir, scope)
	ra.Sequence = 2
	ra.Comments = []Comment{{ID: "gui", Body: "human"}}
	if err := a.Save(ra); err != nil {
		t.Fatal(err)
	}
	rb.Sequence = 1
	rb.Comments = []Comment{{ID: "agent", Body: "agent"}}
	if err := b.Save(rb); err != nil {
		t.Fatal(err)
	}
	// The GUI still lacks the foreign addition. Its next save must retain it.
	ra.Sequence++
	ra.Comments[0].Body = "human edited"
	if err := a.Save(ra); err != nil {
		t.Fatal(err)
	}
	_, got, _ := Open(dir, scope)
	if len(got.Comments) != 2 {
		t.Fatalf("lost a foreign draft: %+v", got)
	}
	c, rc, _ := Open(dir, scope)
	d, rd, _ := Open(dir, scope)
	rc.Sequence++
	rc.Comments[0].Body = "first edit"
	if err := c.Save(rc); err != nil {
		t.Fatal(err)
	}
	rd.Sequence++
	rd.Comments[0].Body = "conflicting edit"
	if err := d.Save(rd); !errors.Is(err, ErrConcurrentWrite) {
		t.Fatalf("concurrent editing was not rejected: %v", err)
	}
}

func TestProcessWriterHelper(t *testing.T) {
	dir := os.Getenv("SIFT_TEST_WRITER_DIR")
	if dir == "" {
		return
	}
	db, record, err := Open(dir, concurrentScope())
	if err != nil {
		panic(err)
	}
	fmt.Println("ready")
	bufio.NewReader(os.Stdin).ReadString('\n')
	record.Sequence = 1
	record.Comments = []Comment{{ID: os.Getenv("SIFT_TEST_WRITER_ID"), Body: "draft"}}
	if err = db.Save(record); err != nil {
		panic(err)
	}
	os.Exit(0)
}

func TestIndependentProcessesCannotOverwriteDrafts(t *testing.T) {
	dir := t.TempDir()
	var commands []*exec.Cmd
	var release []func()
	for _, id := range []string{"gui", "agent"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessWriterHelper$")
		cmd.Env = append(os.Environ(), "SIFT_TEST_WRITER_DIR="+dir, "SIFT_TEST_WRITER_ID="+id)
		stdout, _ := cmd.StdoutPipe()
		stdin, _ := cmd.StdinPipe()
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		if strings.TrimSpace(line) != "ready" {
			t.Fatal("writer did not initialize")
		}
		commands = append(commands, cmd)
		release = append(release, func() { fmt.Fprintln(stdin); stdin.Close() })
	}
	for _, fn := range release {
		fn()
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	_, record, err := Open(dir, concurrentScope())
	if err != nil || len(record.Comments) != 2 {
		t.Fatalf("independent processes lost drafts: %+v %v", record, err)
	}
}
