package test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	fileapp "GopherAI/internal/application/file"
	"GopherAI/internal/infrastructure/storage"
	"GopherAI/pkg/code"
)

// fakeIndexer 实现 domain/rag.Indexer，用于在不依赖 Redis 的情况下测试文件应用服务。
type fakeIndexer struct {
	deleted    []string
	failDelete map[string]bool
}

func (f *fakeIndexer) Index(ctx context.Context, accountNo, storedName, localPath string) error {
	return nil
}

func (f *fakeIndexer) Delete(ctx context.Context, accountNo, storedName string) error {
	if f.failDelete[storedName] {
		return errors.New("index delete failed")
	}
	f.deleted = append(f.deleted, storedName)
	return nil
}

// TestDeleteRagFiles_Success 校验批量删除会移除文件并删除对应向量索引。
func TestDeleteRagFiles_Success(t *testing.T) {
	accountNo := "test_delete_ok_" + strings.ReplaceAll(t.Name(), "/", "_")
	dir, err := storage.UserDocDir(accountNo)
	if err != nil {
		t.Fatalf("UserDocDir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store := storage.NewLocalDocStorage()
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := store.Save(accountNo, name, strings.NewReader("x")); err != nil {
			t.Fatalf("Save(%s) error = %v", name, err)
		}
	}

	idx := &fakeIndexer{}
	svc := fileapp.NewService(store, idx)

	deleted, c := svc.DeleteRagFiles(context.Background(), accountNo, []string{"a.txt", "b.txt"})
	if c != code.CodeSuccess {
		t.Fatalf("DeleteRagFiles() code = %d, want success", c)
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted = %v, want 2", deleted)
	}
	if len(idx.deleted) != 2 {
		t.Fatalf("indexer deleted = %v, want 2", idx.deleted)
	}
	names, _ := store.ListUserDocs(accountNo)
	if len(names) != 0 {
		t.Fatalf("ListUserDocs after delete = %v, want empty", names)
	}
}

// TestDeleteRagFiles_Dedup 校验重复文件名只删除一次。
func TestDeleteRagFiles_Dedup(t *testing.T) {
	accountNo := "test_delete_dedup_" + strings.ReplaceAll(t.Name(), "/", "_")
	dir, _ := storage.UserDocDir(accountNo)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store := storage.NewLocalDocStorage()
	if _, err := store.Save(accountNo, "a.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("Save error = %v", err)
	}
	idx := &fakeIndexer{}
	svc := fileapp.NewService(store, idx)

	deleted, c := svc.DeleteRagFiles(context.Background(), accountNo, []string{"a.txt", "a.txt"})
	if c != code.CodeSuccess || len(deleted) != 1 {
		t.Fatalf("DeleteRagFiles() = (%v, %d), want 1 deleted success", deleted, c)
	}
}

// TestDeleteRagFiles_EmptyRejected 校验空列表返回参数错误。
func TestDeleteRagFiles_EmptyRejected(t *testing.T) {
	t.Parallel()
	svc := fileapp.NewService(storage.NewLocalDocStorage(), &fakeIndexer{})
	if _, c := svc.DeleteRagFiles(context.Background(), "acc", nil); c != code.CodeInvalidParams {
		t.Fatalf("DeleteRagFiles(empty) code = %d, want CodeInvalidParams", c)
	}
}

// TestDeleteRagFiles_AllFail 校验全部删除失败时返回服务端错误。
func TestDeleteRagFiles_AllFail(t *testing.T) {
	accountNo := "test_delete_fail_" + strings.ReplaceAll(t.Name(), "/", "_")
	dir, _ := storage.UserDocDir(accountNo)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store := storage.NewLocalDocStorage()
	if _, err := store.Save(accountNo, "a.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("Save error = %v", err)
	}
	idx := &fakeIndexer{failDelete: map[string]bool{"a.txt": true}}
	svc := fileapp.NewService(store, idx)

	deleted, c := svc.DeleteRagFiles(context.Background(), accountNo, []string{"a.txt"})
	if c != code.CodeServerBusy {
		t.Fatalf("DeleteRagFiles() code = %d, want CodeServerBusy", c)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %v, want empty", deleted)
	}
}

// TestListRagFiles 校验列出当前账号已上传文档：无文档返回空、有文档返回全部。
func TestListRagFiles(t *testing.T) {
	accountNo := "test_list_" + strings.ReplaceAll(t.Name(), "/", "_")
	dir, _ := storage.UserDocDir(accountNo)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store := storage.NewLocalDocStorage()
	svc := fileapp.NewService(store, &fakeIndexer{})

	// 无任何上传时返回空列表且成功。
	files, c := svc.ListRagFiles(accountNo)
	if c != code.CodeSuccess || len(files) != 0 {
		t.Fatalf("ListRagFiles(empty) = (%v, %d), want empty success", files, c)
	}

	for _, name := range []string{"a.txt", "b.md"} {
		if _, err := store.Save(accountNo, name, strings.NewReader("x")); err != nil {
			t.Fatalf("Save(%s) error = %v", name, err)
		}
	}
	files, c = svc.ListRagFiles(accountNo)
	if c != code.CodeSuccess || len(files) != 2 {
		t.Fatalf("ListRagFiles() = (%v, %d), want 2 files success", files, c)
	}
}

// TestRemoveUserDoc 校验按文档删除：成功、幂等、拒绝非法名。
func TestRemoveUserDoc(t *testing.T) {
	accountNo := "test_removedoc_" + strings.ReplaceAll(t.Name(), "/", "_")
	dir, _ := storage.UserDocDir(accountNo)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store := storage.NewLocalDocStorage()
	if _, err := store.Save(accountNo, "doc.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	if err := store.RemoveUserDoc(accountNo, "doc.txt"); err != nil {
		t.Fatalf("RemoveUserDoc() error = %v", err)
	}
	// 幂等：再次删除不存在的文件应成功。
	if err := store.RemoveUserDoc(accountNo, "doc.txt"); err != nil {
		t.Fatalf("RemoveUserDoc() idempotent error = %v", err)
	}
	// 拒绝路径逃逸的文件名。
	if err := store.RemoveUserDoc(accountNo, "../evil.txt"); err == nil {
		t.Fatal("RemoveUserDoc(../evil.txt) error = nil, want error")
	}
}
