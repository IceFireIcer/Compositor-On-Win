/* chromebridge.h — Windows 外壳 seam 的 C 侧：跳转列表的最近文档记录
 * （SHAddToRecentDocs）与任务栏覆盖图标（ITaskbarList3::SetOverlayIcon）。这两件
 * 事都要原始 COM 指针和顶层窗口枚举，go vet 的 unsafeptr 检查表达不了，所以和
 * internal/winclip 一样放进 C++，Go 侧只翻译错误码。
 *
 * 约定：所有函数都不打印、不抛异常；返回值就是错误分类，中文提示由 Go 侧给。
 *
 *   0 CHROME_OK           成功
 *   1 CHROME_ERR_PATH     路径为空或非法
 *   2                     （保留：原"外壳拒绝"分类。SHAddToRecentDocs 返回 void，
 *                          SetOverlayIcon 有自己的失败码，没有函数会返回它）
 *   3 CHROME_ERR_WINDOW   找不到本进程的主窗口（窗口还没建好、非 GUI 进程）
 *   4 CHROME_ERR_TASKBAR  ITaskbarList3 不可用（Windows 7 以下、COM 失败）
 *   5 CHROME_ERR_ICON     覆盖图标创建失败（GDI）
 *   6 CHROME_ERR_OVERLAY  SetOverlayIcon 调用失败
 *   7 CHROME_ERR_NOT_SHOWN 主窗口已经建好但还没显示（还没有任务栏按钮可画）
 */
#ifndef CHROMEBRIDGE_H
#define CHROMEBRIDGE_H

#include <wchar.h> /* wchar_t */

#ifdef __cplusplus
extern "C" {
#endif

#define CHROME_OK 0
#define CHROME_ERR_PATH 1
#define CHROME_ERR_WINDOW 3
#define CHROME_ERR_TASKBAR 4
#define CHROME_ERR_ICON 5
#define CHROME_ERR_OVERLAY 6
#define CHROME_ERR_NOT_SHOWN 7

/* 把一个文件记进本应用的"最近文档"（SHAddToRecentDocs(SHARD_PATHW, path)）。
 * path 是 UTF-16、以 NUL 结尾，只在该次调用期间有效。SHAddToRecentDocs 返回
 * void，所以除参数检查外没有别的失败信号：非空路径即返回 CHROME_OK。 */
int chrome_note_recent(const wchar_t* path);

/* 清空本应用记的最近文档（SHAddToRecentDocs(SHARD_PIDL, NULL)）。总是
 * CHROME_OK——这个调用没有失败路径。 */
int chrome_clear_recent(void);

/* on != 0 时在进程主窗口上显示缓存的红点覆盖图标，on == 0 时传 NULL 清除。
 * 主窗口由本函数自己找：EnumWindows + 进程 pid 比对 + 标题非空，优先可见的那个，
 * 找不到可见的就退到"已建好但还没显示、无属主、带 WS_EX_APPWINDOW"的那个
 * （Wails 在导航完成前不显示主窗口，启动时的同步必然撞上这一瞬）。
 *
 * 只有隐藏候选时返回 CHROME_ERR_NOT_SHOWN 而**不**调用 SetOverlayIcon：没有任务栏
 * 按钮可画，MSDN 也不保证这种提前调用会被保留。 */
int chrome_set_overlay(int on);

#ifdef __cplusplus
}
#endif

#endif /* CHROMEBRIDGE_H */
