package bridge

// 撤销/重做的操作名（对等矩阵 C10）。原版的菜单标题是 "Undo <操作名>"，没有可撤销
// 项时退回裸 "Undo"（CompositorApp.swift:43-45）。Go 侧的操作名一直有
// （History.UndoName/RedoName，history.go:113,122），缺的只是把它送到前端的通路 ——
// 所以菜单此前只显示没有操作名的裸标题。

import "encoding/json"

// historyNamesReply 是撤销/重做查询的回包。名字只在真的可撤销/可重做时才给，
// 免得前端拿着栈底留下的旧名字去拼一个不存在的操作。
type historyNamesReply struct {
	CanUndo  bool   `json:"canUndo"`
	UndoName string `json:"undoName"`
	CanRedo  bool   `json:"canRedo"`
	RedoName string `json:"redoName"`
}

// HistoryNames 报告活动会话的历史栈顶操作名。
func (s *Service) HistoryNames() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.doc == nil {
		return "", errNoDocument
	}
	reply := historyNamesReply{
		CanUndo: sess.hist.CanUndo(),
		CanRedo: sess.hist.CanRedo(),
	}
	if reply.CanUndo {
		reply.UndoName = sess.hist.UndoName()
	}
	if reply.CanRedo {
		reply.RedoName = sess.hist.RedoName()
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
