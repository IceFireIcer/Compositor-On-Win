package bridge

// 最近项目（票 49）——RecentProjects.swift 的 Windows 版：macOS 那份是
// NSDocumentController 的最近文档（Dock 菜单显示同一份），这里落一个 JSON 列表
// （用户配置目录 compositor-on-windows/recent.json），并与 Windows 外壳的"最近
// 文档"（任务栏跳转列表）同步。
//
// 与 Swift 一致的两点：列表按最近优先、大小写不敏感去重、上限 10；读取时把已经
// 移动/删除的项目过滤掉（RecentProjects.refresh 的 fileExists 过滤）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"compositor-win/internal/fsatomic"
)

// defaultRecentLimit 是最近项目列表的上限（macOS 的最近文档菜单也是 10 条）。
const defaultRecentLimit = 10

// caseSensitiveFS 标记路径大小写敏感的平台：Windows 上 C:\a.comp 与 c:\A.COMP
// 是同一个项目（Swift 用 URL 相等，Windows 语义按大小写不敏感处理）。
const caseSensitiveFS = runtime.GOOS != "windows"

// RecentStore 持久化最近项目列表。Wails 每个绑定调用都在自己的 goroutine 里跑，
// 所以读写用一把锁串起来（原版是 @MainActor）—— 两个"打开"同时记一笔时不能交错
// 写同一个 JSON 文件。
type RecentStore struct {
	mu    sync.Mutex
	path  string
	limit int
}

// recentState 是落盘形状。
type recentState struct {
	Paths []string `json:"paths"`
}

// NewRecentStoreAt 指向一个显式路径（测试用）。
func NewRecentStoreAt(path string) *RecentStore {
	return &RecentStore{path: path, limit: defaultRecentLimit}
}

// DefaultRecentStore 指向每用户配置目录。
func DefaultRecentStore() (*RecentStore, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("无法定位用户配置目录: %w", err)
	}
	return NewRecentStoreAt(filepath.Join(base, "compositor-on-windows", "recent.json")), nil
}

// Add 把 path 放到列表最前（大小写不敏感去重、截到上限）并落盘。
func (s *RecentStore) Add(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("最近项目路径为空")
	}
	list, err := s.read()
	if err != nil {
		return err
	}
	next := []string{trimmed}
	for _, existing := range list {
		if !samePath(existing, trimmed) {
			next = append(next, existing)
		}
	}
	if len(next) > s.limit {
		next = next[:s.limit]
	}
	return s.write(next)
}

// List 返回仍然存在的项目（保序、大小写不敏感去重、截到上限）。
func (s *RecentStore) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, candidate := range list {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		key := filepath.Clean(trimmed)
		if !caseSensitiveFS {
			key = strings.ToLower(key)
		}
		if seen[key] {
			continue
		}
		if _, err := os.Stat(trimmed); err != nil {
			continue // 移动/删除过的项目不再出现（RecentProjects.refresh）
		}
		seen[key] = true
		out = append(out, trimmed)
		if len(out) >= s.limit {
			break
		}
	}
	return out, nil
}

// Clear 清空列表并落盘。
func (s *RecentStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(nil)
}

// read 读列表：文件不存在算空列表；内容坏了**不报错**——把坏文件改名成 .bak 后
// 当作空列表（一个坏文件不该让"最近项目"永久失效，菜单静静退化成干净的列表）。
func (s *RecentStore) read() ([]string, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取最近项目失败: %w", err)
	}
	var state recentState
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Printf("最近项目文件损坏，已改名为 %s.bak: %v\n", s.path, err)
		_ = os.Rename(s.path, s.path+".bak")
		return nil, nil
	}
	return state.Paths, nil
}

// write 落盘（目录按需创建）。先写临时文件再改名 —— 与 project.Store 的原子保存
// 同一套路，崩在中间也不会留下半个 JSON。
func (s *RecentStore) write(paths []string) error {
	data, err := json.MarshalIndent(recentState{Paths: paths}, "", "  ")
	if err != nil {
		return fmt.Errorf("无法编码最近项目: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("无法创建配置目录: %w", err)
	}
	// 一次 rename 覆盖（Windows 上也允许，见 internal/fsatomic）：先删再改名会在
	// "删成功、改名失败"时把列表整个丢掉。
	if err := fsatomic.WriteFile(s.path, data, 0o644); err != nil {
		return fmt.Errorf("提交最近项目失败: %w", err)
	}
	return nil
}

// samePath 比较两个路径（Windows 上大小写不敏感；Clean 掉 ./ 之类）。Add 与 List
// 共用这一条判等规则，免得两处漂移。
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
