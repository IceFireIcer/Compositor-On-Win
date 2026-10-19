/**
 * C48（票 53）：图层菜单「移出文件夹」的接线测试。
 *
 * 这里只钉前端这一小段：菜单可用性来自 layerMenuState，动作把 Go 的
 * moveOutOfGroup op 发给 LayerOp（payload 为空 —— op 自己取活动图层）。
 * 真正的文档语义（parentID/扁平位置/撤销）由 internal/bridge 的
 * c48_move_out_of_group_test.go 负责。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as BridgeService from "../../../wailsjs/go/bridge/Service";
import {
  EMPTY_DOCUMENT,
  document,
  setDocumentService,
  type DocLayer,
  type DocumentState,
} from "../state/document";
import { layerMenuState, moveActiveLayerOutOfGroup } from "./actions";

function layer(id: string, parentID: string | null, isGroup = false): DocLayer {
  return {
    id,
    name: id,
    isVisible: true,
    isGroup,
    opacity: 1,
    blendMode: "Normal",
    parentID,
    maskSourceID: null,
    imageFile: null,
    adjustment: null,
    text: null,
    transform: { origin: [0, 0], size: [16, 16] },
  };
}

function state(layers: DocLayer[], activeLayerID: string | null): DocumentState {
  return {
    ...EMPTY_DOCUMENT,
    docId: layers.length > 0 || activeLayerID !== null ? "doc-1" : null,
    width: 16,
    height: 16,
    activeLayerID,
    layers,
  };
}

// 与当前 store（rev 0）同 rev/docId 的快照：applyDocumentSnapshot 会把它当成
// 过期回复直接忽略，测试不会触发 workspace 刷新等副作用。
const IGNORED_SNAPSHOT =
  '{"rev":0,"filterRev":0,"doc":{"documentID":"doc-1","width":16,"height":16,' +
  '"resolution":72,"activeLayerID":"inner","layers":[]}}';

describe("Move Out of Folder（C48）", () => {
  const layerOp = vi.fn();

  beforeEach(() => {
    layerOp.mockReset();
    setDocumentService({ DocumentSnapshot: vi.fn(), LayerOp: layerOp });
  });

  afterEach(() => {
    document.set({ ...EMPTY_DOCUMENT });
    setDocumentService(BridgeService);
  });

  it("活动图层在文件夹里时发 Go 的 moveOutOfGroup op（payload 为空）", async () => {
    layerOp.mockResolvedValue(IGNORED_SNAPSHOT);
    document.set(state([layer("folder", null, true), layer("inner", "folder")], "inner"));

    await moveActiveLayerOutOfGroup();

    expect(layerOp).toHaveBeenCalledTimes(1);
    expect(layerOp).toHaveBeenCalledWith("moveOutOfGroup", "{}");
  });

  it("根级图层不发送 op（菜单同样禁用）", async () => {
    document.set(state([layer("root", null)], "root"));
    expect(layerMenuState(state([layer("root", null)], "root")).canMoveOutOfGroup).toBe(false);

    await moveActiveLayerOutOfGroup();

    expect(layerOp).not.toHaveBeenCalled();
  });

  it("菜单可用性只取决于活动图层有没有父级", () => {
    expect(
      layerMenuState(state([layer("folder", null, true), layer("inner", "folder")], "inner"))
        .canMoveOutOfGroup,
    ).toBe(true);
    expect(layerMenuState(state([layer("folder", null, true), layer("inner", "folder")], "folder"))
      .canMoveOutOfGroup).toBe(false);
    expect(layerMenuState({ ...EMPTY_DOCUMENT }).canMoveOutOfGroup).toBe(false);
  });
});
