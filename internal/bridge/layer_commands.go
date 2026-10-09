package bridge

// The layer command family (ticket 47): every C46–C53 layer-menu command that
// rewrites the document. frontend/src/lib/state/layerOps.ts holds the pure
// functions this file mirrors — same plans, names, slots and removals — and the
// Swift sources are the algorithm truth:
//
//	LayerGroups.swift        groupSelectedLayers (167), ungroupLayers (220)
//	LayerMerge.swift         mergePlan (7), mergeLayers (34)
//	LiveLayerMask.swift      validate (3), canLinkMask (23), linkMask (31),
//	                         removeLiveMask (53), releaseDetachedClipping (225)
//	SelectionClipboard.swift duplicateLayers (187), insertCopy (213)
//	LayerMask.swift          deleteLayerMask (287)
//	LayerEffects.swift       remove (182), removeSelectedEffect (357)
//	EditorSession.swift      nudgeLayer (537), commitTransform's edit name (485)
//
// Every op validates its whole plan against a candidate copy of the document
// before committing anything, so a rejected request leaves sess.doc
// byte-identical; Workspace.EditActive rolls the document back as a second
// line of defence.
//
// One deliberate difference from the ticket text: the bitmaps of layers a
// command removes stay in sess.bitmaps. Go's history stores the manifest only
// (history.Snapshot), so dropping the pixels would leave Undo with layers whose
// assets no longer exist — deleteLayer already keeps them for the same reason.
// A pixel journal like filter's rasterJournal would be the thorough fix.

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// maxTransformSide / maxTransformOrigin / maxNudgeDelta mirror
// LayerTransform.isValid (LayerTransform.swift:27-31) and the arrow-key nudge
// range the bridge accepts.
const (
	maxTransformSide   = 300_000
	maxTransformOrigin = 1_000_000
	maxNudgeDelta      = 1_000_000
)

// decodeLayerPayload parses one op's own payload shape. planLayerOp already
// rejected malformed JSON; each op re-reads the raw string so document.go's
// switch stays a pure dispatch table.
func decodeLayerPayload(payload string, out any) error {
	if trimmed := strings.TrimSpace(payload); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), out); err != nil {
			return fmt.Errorf("无法解析 payload: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tree helpers (the flat bottom→top array with parentID lookup)

// layerAt finds a layer by ID, nil when it is gone.
func layerAt(doc *domain.Document, id string) *domain.Layer {
	if idx := findLayerIndex(doc, id); idx >= 0 {
		return &doc.Layers[idx]
	}
	return nil
}

// parentIDOf returns a layer's parent, "" at the root level (layerOps.ts' "?? null").
func parentIDOf(l *domain.Layer) string {
	if l == nil || l.ParentID == nil {
		return ""
	}
	return *l.ParentID
}

// pointsTo reports whether an optional id reference names id.
func pointsTo(ref *string, id string) bool { return ref != nil && *ref == id }

// idPtr returns a fresh pointer to id, so records never share one mutable slot.
func idPtr(id string) *string { return &id }

// cloneIDPtr copies an optional id; nil stays nil.
func cloneIDPtr(id *string) *string {
	if id == nil {
		return nil
	}
	return idPtr(*id)
}

// layerChildren maps parent id → child indexes in flat (bottom→top) order.
func layerChildren(doc *domain.Document) map[string][]int {
	children := make(map[string][]int, len(doc.Layers))
	for i := range doc.Layers {
		key := parentIDOf(&doc.Layers[i])
		children[key] = append(children[key], i)
	}
	return children
}

// siblingIndexes lists the flat indexes sharing parentID, in flat order.
func siblingIndexes(doc *domain.Document, parentID string) []int {
	var out []int
	for i := range doc.Layers {
		if parentIDOf(&doc.Layers[i]) == parentID {
			out = append(out, i)
		}
	}
	return out
}

// descendantIDsOf returns every layer below id in the tree (not id itself) —
// Swift descendantIDs(of:) / layerOps.ts descendantIDs.
func descendantIDsOf(doc *domain.Document, id string) map[string]bool {
	children := layerChildren(doc)
	out := map[string]bool{}
	pending := []string{id}
	for len(pending) > 0 {
		parent := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, idx := range children[parent] {
			child := doc.Layers[idx].ID
			if !out[child] {
				out[child] = true
				pending = append(pending, child)
			}
		}
	}
	return out
}

// hierarchyOrder lists ids depth-first pre-order, bottom→top (Swift
// LayerOrder.resolve / layerOps.ts hierarchyOrder).
func hierarchyOrder(doc *domain.Document) []string {
	children := layerChildren(doc)
	var order []string
	var visit func(parent string)
	visit = func(parent string) {
		for _, idx := range children[parent] {
			l := &doc.Layers[idx]
			order = append(order, l.ID)
			if l.IsGroupLayer() {
				visit(l.ID)
			}
		}
	}
	visit("")
	return order
}

// panelOrder lists ids as the layer panel shows them, top first (Swift
// LayerHierarchy.entries topFirst / layerOps.ts panelOrder) — the order the
// duplicate restack compares layers in.
func panelOrder(doc *domain.Document) []string {
	children := layerChildren(doc)
	var out []string
	var visit func(parent string)
	visit = func(parent string) {
		list := children[parent]
		for i := len(list) - 1; i >= 0; i-- {
			l := &doc.Layers[list[i]]
			out = append(out, l.ID)
			if l.IsGroupLayer() {
				visit(l.ID)
			}
		}
	}
	visit("")
	return out
}

// ancestorsOf lists a layer's ancestors from its parent up to the root level
// (""), which is always last — layerOps.ts ancestorsOf.
func ancestorsOf(doc *domain.Document, id string) []string {
	var chain []string
	seen := map[string]bool{id: true}
	for parent := parentIDOf(layerAt(doc, id)); parent != ""; parent = parentIDOf(layerAt(doc, parent)) {
		if seen[parent] { // a cycle cannot happen in a validated document
			break
		}
		seen[parent] = true
		chain = append(chain, parent)
	}
	return append(chain, "")
}

// commonAncestor returns the deepest ancestor every id shares, "" when they
// only meet at the root level (LayerGroups.swift:181-183).
func commonAncestor(doc *domain.Document, ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	for _, candidate := range ancestorsOf(doc, ids[0]) {
		shared := true
		for _, id := range ids {
			if !containsString(ancestorsOf(doc, id), candidate) {
				shared = false
				break
			}
		}
		if shared {
			return candidate
		}
	}
	return ""
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// nextFreeName returns the next unused "<prefix> N" (layerOps.ts nextName /
// Swift groupSelectedLayers' folder counter).
func nextFreeName(doc *domain.Document, prefix string) string {
	names := make(map[string]bool, len(doc.Layers))
	for i := range doc.Layers {
		names[doc.Layers[i].Name] = true
	}
	for n := 1; ; n++ {
		candidate := fmt.Sprintf("%s %d", prefix, n)
		if !names[candidate] {
			return candidate
		}
	}
}

// newGroupLayer builds a folder record: no image asset, a full-canvas
// transform, visible, Normal at full opacity — Swift's
// ImageLayer(name:blankSize:) with isGroup set (LayerGroups.swift:187-189).
func newGroupLayer(name string, width, height int) domain.Layer {
	isGroup := true
	opacity := 1.0
	blend := domain.BlendNormal
	return domain.Layer{
		ID:        newUUID(),
		Name:      name,
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{0, 0},
			Size:     [2]float64{float64(width), float64(height)},
			Sampling: domain.SamplingNearest,
		},
		IsGroup:   &isGroup,
		Opacity:   &opacity,
		BlendMode: &blend,
	}
}

// cloneLayerRecord deep-copies one layer record via a JSON round trip (the
// cloneDocForEdit trick), so a copy never aliases the original's effects,
// adjustment or text payloads.
func cloneLayerRecord(l domain.Layer) domain.Layer {
	b, err := json.Marshal(l)
	if err != nil {
		return l
	}
	var out domain.Layer
	if err := json.Unmarshal(b, &out); err != nil {
		return l
	}
	return out
}

// validLayerTransform mirrors project.transformValid (LayerTransform.swift:27-31):
// finite numbers, sides 1–300,000, origin within ±1,000,000 document pixels and
// one of the three sampling spellings.
func validLayerTransform(t domain.Transform) bool {
	switch t.Sampling {
	case domain.SamplingNearest, domain.SamplingSmooth, domain.SamplingHighQuality:
	default:
		return false
	}
	for _, v := range []float64{t.Origin[0], t.Origin[1], t.Size[0], t.Size[1], t.Rotation} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return t.Size[0] >= 1 && t.Size[0] <= maxTransformSide &&
		t.Size[1] >= 1 && t.Size[1] <= maxTransformSide &&
		math.Abs(t.Origin[0]) <= maxTransformOrigin && math.Abs(t.Origin[1]) <= maxTransformOrigin
}

// ---------------------------------------------------------------------------
// Clipping-mask graph (LiveLayerMask.swift)

// validateClipGraph ports Swift LiveMaskGraph.validate (LiveLayerMask.swift:3-20):
// every clip chain stays within domain.MaxClipChain nodes, never cycles, and no
// link hangs off a group or an adjustment layer. domain.Validate covers the
// cycle, limit and group rules; this adds the adjustment-source rule and the
// whole-chain walk the original performs.
func validateClipGraph(doc *domain.Document) error {
	byID := make(map[string]*domain.Layer, len(doc.Layers))
	for i := range doc.Layers {
		byID[doc.Layers[i].ID] = &doc.Layers[i]
	}
	for i := range doc.Layers {
		root := doc.Layers[i].ID
		path := map[string]bool{}
		current := &doc.Layers[i]
		for {
			if len(path) >= domain.MaxClipChain {
				return fmt.Errorf("图层 %s 的剪贴链超过 %d 节点", root, domain.MaxClipChain)
			}
			if path[current.ID] {
				return fmt.Errorf("图层 %s 的剪贴链形成循环", root)
			}
			path[current.ID] = true
			if current.MaskSourceID == nil {
				break
			}
			if current.IsGroupLayer() {
				return fmt.Errorf("编组 %s 不能引用剪贴蒙版来源", current.ID)
			}
			source := byID[*current.MaskSourceID]
			if source == nil {
				return fmt.Errorf("图层 %s 的剪贴蒙版来源 %s 不存在", current.ID, *current.MaskSourceID)
			}
			if source.IsGroupLayer() {
				return fmt.Errorf("图层 %s 的剪贴蒙版来源 %s 是编组", current.ID, source.ID)
			}
			if source.Adjustment != nil {
				return fmt.Errorf("图层 %s 的剪贴蒙版来源 %s 是调整图层", current.ID, source.ID)
			}
			current = source
		}
	}
	return nil
}

// adoptClipping ports Swift adoptClipping (LiveLayerMask.swift:42-51): a layer
// dropped into the middle of a clipping group joins it. Run while rearranging,
// before releaseDetachedClipping.
func adoptClipping(doc *domain.Document, id string) {
	index := findLayerIndex(doc, id)
	if index < 0 || doc.Layers[index].IsGroupLayer() {
		return
	}
	layer := &doc.Layers[index]
	siblings := siblingIndexes(doc, parentIDOf(layer))
	position := -1
	for i, idx := range siblings {
		if idx == index {
			position = i
			break
		}
	}
	if position <= 0 || position+1 >= len(siblings) {
		return
	}
	source := doc.Layers[siblings[position+1]].MaskSourceID
	if source == nil || *source == id {
		return
	}
	below := &doc.Layers[siblings[position-1]]
	if below.ID != *source && !pointsTo(below.MaskSourceID, *source) {
		return
	}
	layer.MaskSourceID = idPtr(*source)
}

// releaseDetachedClipping ports Swift releaseDetachedClipping
// (LiveLayerMask.swift:225-237): a layer stops clipping when it no longer
// belongs to the contiguous stack above its base.
func releaseDetachedClipping(doc *domain.Document) {
	grouped := map[string][]int{}
	var order []string
	for i := range doc.Layers {
		key := parentIDOf(&doc.Layers[i])
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], i)
	}
	release := map[string]bool{}
	for _, key := range order {
		base := ""
		for _, idx := range grouped[key] {
			l := &doc.Layers[idx]
			if l.MaskSourceID != nil {
				if *l.MaskSourceID != base {
					release[l.ID] = true
					base = l.ID
				}
				continue
			}
			if l.IsGroupLayer() {
				base = ""
			} else {
				base = l.ID
			}
		}
	}
	for i := range doc.Layers {
		if release[doc.Layers[i].ID] {
			doc.Layers[i].MaskSourceID = nil
		}
	}
}

// placeWithin ports layerOps.ts placeWithin / Swift placeLayer
// (LayerGroups.swift:285): id moves to just above aboveID ("" = the top of the
// list) inside parentID ("" = the root level), with the clipping fixes the
// original runs while rearranging.
func placeWithin(doc *domain.Document, id, parent, above string) bool {
	index := findLayerIndex(doc, id)
	if index < 0 || above == id {
		return false
	}
	target := -1
	if above != "" {
		target = findLayerIndex(doc, above)
		if target < 0 || parentIDOf(&doc.Layers[target]) != parent {
			return false
		}
	}
	moving := doc.Layers[index]
	without := make([]domain.Layer, 0, len(doc.Layers)-1)
	without = append(without, doc.Layers[:index]...)
	without = append(without, doc.Layers[index+1:]...)
	moving.ParentID = nil
	if parent != "" {
		moving.ParentID = idPtr(parent)
	}
	at := len(without)
	if target >= 0 {
		at = target
		if target <= index {
			at = target + 1
		}
	}
	out := make([]domain.Layer, 0, len(doc.Layers))
	out = append(out, without[:at]...)
	out = append(out, moving)
	out = append(out, without[at:]...)
	doc.Layers = out
	adoptClipping(doc, id)
	releaseDetachedClipping(doc)
	return true
}

// ---------------------------------------------------------------------------
// duplicateLayer — ⌘J (SelectionClipboard.swift:187 duplicateLayers, :213 insertCopy)

type duplicateLayerPayload struct {
	ID  string   `json:"id"`
	IDs []string `json:"ids"`
}

func planDuplicateLayer(payload string) (string, func(*session) error, error) {
	var p duplicateLayerPayload
	if err := decodeLayerPayload(payload, &p); err != nil {
		return "", nil, err
	}
	if p.ID == "" && len(p.IDs) == 0 {
		return "", nil, fmt.Errorf("duplicateLayer 缺少 id")
	}
	return "Duplicate Layer", func(sess *session) error {
		return duplicateLayersInSession(sess, p.ID, p.IDs)
	}, nil
}

// duplicateLayersInSession ports Swift duplicateLayers (SelectionClipboard.swift:187-210):
// every given root — a folder brings its contents — is copied just above
// itself; several copies restack together above the topmost original, in flat
// order. The copies end up active.
func duplicateLayersInSession(sess *session, id string, ids []string) error {
	doc := sess.doc
	present := make(map[string]bool, len(doc.Layers))
	for i := range doc.Layers {
		present[doc.Layers[i].ID] = true
	}
	var roots []string
	if len(ids) > 0 {
		for _, candidate := range ids {
			if present[candidate] { // unknown ids drop out, as in layerOps.ts
				roots = append(roots, candidate)
			}
		}
	} else if present[id] {
		roots = []string{id}
	}
	if len(roots) == 0 {
		return fmt.Errorf("没有可复制的图层")
	}

	candidate := cloneDocForEdit(doc)
	newBitmaps := map[string]*render.Bitmap{}
	copiesOf := map[string]string{}
	for _, root := range roots {
		copyID, err := insertCopy(sess, candidate, root, newBitmaps)
		if err != nil {
			return err
		}
		copiesOf[root] = copyID
	}
	if len(copiesOf) > 1 {
		// Panel order, top first, so layers in different folders compare as
		// they are seen (SelectionClipboard.swift:196).
		var originals []string
		for _, original := range panelOrder(candidate) {
			if _, ok := copiesOf[original]; ok {
				originals = append(originals, original)
			}
		}
		if len(originals) > 0 {
			top := originals[0]
			parent := parentIDOf(layerAt(candidate, top))
			below := top
			for i := len(originals) - 1; i >= 0; i-- {
				copyID := copiesOf[originals[i]]
				if placeWithin(candidate, copyID, parent, below) {
					below = copyID
				}
			}
		}
	}

	active := copiesOf[derefString(doc.ActiveLayerID)]
	if active == "" {
		active = copiesOf[roots[0]]
	}
	if active != "" {
		candidate.ActiveLayerID = idPtr(active)
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	sess.doc.Layers = candidate.Layers
	sess.doc.ActiveLayerID = candidate.ActiveLayerID
	for name, bmp := range newBitmaps {
		sess.bitmaps[name] = bmp
	}
	return nil
}

// insertCopy ports Swift insertCopy (SelectionClipboard.swift:213-233): one
// copy of the layer and everything it holds, inserted just above it. The
// copied root gains " copy", descendants keep their names, clip sources inside
// the subtree are remapped into the copy, and the image/mask assets get fresh
// <newID> names with independent pixels — the package contract ties asset
// names to the layer UUID (project.imageName).
func insertCopy(sess *session, doc *domain.Document, id string, newBitmaps map[string]*render.Bitmap) (string, error) {
	index := findLayerIndex(doc, id)
	if index < 0 {
		return "", fmt.Errorf("图层不存在: %s", id)
	}
	included := descendantIDsOf(doc, id)
	included[id] = true
	var originals []domain.Layer
	for i := range doc.Layers {
		if included[doc.Layers[i].ID] {
			originals = append(originals, doc.Layers[i])
		}
	}
	if len(doc.Layers)+len(originals) > domain.MaxLayers {
		return "", fmt.Errorf("图层数超过 %d 上限，无法复制", domain.MaxLayers)
	}
	mapping := make(map[string]string, len(originals))
	for _, original := range originals {
		mapping[original.ID] = newUUID()
	}
	copies := make([]domain.Layer, 0, len(originals))
	for _, original := range originals {
		copied := cloneLayerRecord(original)
		copied.ID = mapping[original.ID]
		if original.ID == id {
			copied.Name = original.Name + " copy"
		}
		if original.ParentID != nil {
			if mapped, ok := mapping[*original.ParentID]; ok {
				copied.ParentID = idPtr(mapped)
			}
		}
		if original.MaskSourceID != nil {
			if mapped, ok := mapping[*original.MaskSourceID]; ok {
				copied.MaskSourceID = idPtr(mapped)
			}
		}
		if err := copyLayerAssets(sess, &copied, newBitmaps); err != nil {
			return "", err
		}
		copies = append(copies, copied)
	}
	out := make([]domain.Layer, 0, len(doc.Layers)+len(copies))
	out = append(out, doc.Layers[:index+1]...)
	out = append(out, copies...)
	out = append(out, doc.Layers[index+1:]...)
	doc.Layers = out
	return mapping[id], nil
}

// copyLayerAssets gives a copied layer its own <newID>.png / <newID>.mask.png
// assets, backed by deep bitmap copies, so painting one layer never touches the
// other.
func copyLayerAssets(sess *session, layer *domain.Layer, newBitmaps map[string]*render.Bitmap) error {
	if layer.ImageFile != nil {
		if err := cloneAsset(sess, derefString(layer.ImageFile), layer.ID+".png", newBitmaps); err != nil {
			return err
		}
		layer.ImageFile = idPtr(layer.ID + ".png")
	}
	if layer.MaskFile != nil {
		if err := cloneAsset(sess, derefString(layer.MaskFile), layer.ID+".mask.png", newBitmaps); err != nil {
			return err
		}
		layer.MaskFile = idPtr(layer.ID + ".mask.png")
	}
	return nil
}

// cloneAsset stages a deep copy of one asset under a new name; the caller
// publishes the staged bitmaps only once the whole op is valid.
func cloneAsset(sess *session, oldName, newName string, newBitmaps map[string]*render.Bitmap) error {
	source, ok := sess.bitmaps[oldName]
	if !ok || source == nil {
		return fmt.Errorf("内存位图库缺少资产 %s，无法复制", oldName)
	}
	newBitmaps[newName] = source.Clone()
	return nil
}

// ---------------------------------------------------------------------------
// groupLayers / ungroupLayer — ⌘G / ⇧⌘G (LayerGroups.swift)

type groupLayersPayload struct {
	ID  string   `json:"id"`
	IDs []string `json:"ids"`
}

func planGroupLayers(payload string) (string, func(*session) error, error) {
	var p groupLayersPayload
	if err := decodeLayerPayload(payload, &p); err != nil {
		return "", nil, err
	}
	if len(p.IDs) == 0 && p.ID == "" {
		return "", nil, fmt.Errorf("groupLayers 缺少 ids")
	}
	return "Group Layers", func(sess *session) error {
		ids := p.IDs
		if len(ids) == 0 {
			ids = []string{p.ID}
		}
		return groupSelectedLayers(sess, ids, p.ID)
	}, nil
}

// groupSelectedLayers ports Swift groupSelectedLayers (LayerGroups.swift:167-212):
// the selected roots (a selected folder carries its subtree, a selected
// descendant is not pulled out of it) move into a new "Folder N" that takes the
// topmost selected branch's slot in their nearest common ancestor, and the
// folder becomes active. A selection spanning folders is not an error: the
// wrapper goes to the common ancestor, exactly as the original does.
func groupSelectedLayers(sess *session, ids []string, activeHint string) error {
	doc := sess.doc
	if len(doc.Layers) >= domain.MaxLayers {
		return fmt.Errorf("图层数已达 %d 上限，无法编组", domain.MaxLayers)
	}
	present := make(map[string]bool, len(doc.Layers))
	for i := range doc.Layers {
		present[doc.Layers[i].ID] = true
	}
	if activeHint != "" && !present[activeHint] {
		return fmt.Errorf("图层不存在: %s", activeHint)
	}
	selected := map[string]bool{}
	for _, id := range ids {
		if present[id] {
			selected[id] = true
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("没有可编组的图层")
	}
	roots := map[string]bool{}
	for id := range selected {
		nested := false
		for _, ancestor := range ancestorsOf(doc, id) {
			if ancestor != "" && selected[ancestor] {
				nested = true
				break
			}
		}
		if !nested {
			roots[id] = true
		}
	}
	var ordered []string
	for _, id := range hierarchyOrder(doc) {
		if roots[id] {
			ordered = append(ordered, id)
		}
	}
	if len(ordered) == 0 {
		return fmt.Errorf("没有可编组的图层")
	}
	parent := commonAncestor(doc, ordered)
	group := newGroupLayer(nextFreeName(doc, "Folder"), doc.Width, doc.Height)
	if parent != "" {
		group.ParentID = idPtr(parent)
	}
	// Each selected layer climbs to the branch under the common parent; the
	// wrapper lands after the highest of those branches (LayerGroups.swift:190-197).
	branches := map[string]bool{}
	for _, id := range ordered {
		branch := id
		for {
			p := parentIDOf(layerAt(doc, branch))
			if p == "" || p == parent {
				break
			}
			branch = p
		}
		branches[branch] = true
	}
	highest := -1
	for i := len(doc.Layers) - 1; i >= 0; i-- {
		if branches[doc.Layers[i].ID] {
			highest = i
			break
		}
	}
	insertion := len(doc.Layers)
	if highest >= 0 {
		insertion = 0
		for i := 0; i <= highest; i++ {
			if !roots[doc.Layers[i].ID] {
				insertion++
			}
		}
	}
	var kept []domain.Layer
	for i := range doc.Layers {
		if !roots[doc.Layers[i].ID] {
			kept = append(kept, doc.Layers[i])
		}
	}
	if insertion > len(kept) {
		insertion = len(kept)
	}
	out := make([]domain.Layer, 0, len(kept)+1+len(ordered))
	out = append(out, kept[:insertion]...)
	out = append(out, group)
	out = append(out, kept[insertion:]...)
	// The selected roots become the folder's children; the original appends
	// them so the renderers' parent lookup finds them in flat order.
	for _, id := range ordered {
		child := *layerAt(doc, id)
		child.ParentID = idPtr(group.ID)
		out = append(out, child)
	}
	candidate := *doc
	candidate.Layers = out
	candidate.ActiveLayerID = idPtr(group.ID)
	if err := validateClipGraph(&candidate); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	sess.doc.Layers = out
	sess.doc.ActiveLayerID = idPtr(group.ID)
	return nil
}

type ungroupLayerPayload struct {
	ID string `json:"id"`
}

func planUngroupLayer(payload string) (string, func(*session) error, error) {
	var p ungroupLayerPayload
	if err := decodeLayerPayload(payload, &p); err != nil {
		return "", nil, err
	}
	if p.ID == "" {
		return "", nil, fmt.Errorf("ungroupLayer 缺少 id")
	}
	return "Ungroup Layers", func(sess *session) error {
		return ungroupLayerInSession(sess, p.ID)
	}, nil
}

// ungroupLayerInSession ports Swift ungroupLayers (LayerGroups.swift:220-239):
// the folder's direct children take its place among its own siblings, in the
// order they had inside it, and the folder goes — its own opacity, blend mode,
// mask and effects are discarded with it. The first child (which now sits in
// the folder's slot) becomes active; clipping that no longer sits above its
// base is released.
func ungroupLayerInSession(sess *session, id string) error {
	doc := sess.doc
	index := findLayerIndex(doc, id)
	if index < 0 {
		return fmt.Errorf("图层不存在: %s", id)
	}
	if !doc.Layers[index].IsGroupLayer() {
		return fmt.Errorf("图层 %s 不是编组，无法解散", id)
	}
	group := doc.Layers[index]
	childIDs := map[string]bool{}
	var children []domain.Layer
	for i := range doc.Layers {
		if pointsTo(doc.Layers[i].ParentID, id) {
			child := doc.Layers[i]
			child.ParentID = cloneIDPtr(group.ParentID)
			children = append(children, child)
			childIDs[child.ID] = true
		}
	}
	out := make([]domain.Layer, 0, len(doc.Layers))
	for i := range doc.Layers {
		switch {
		case doc.Layers[i].ID == id:
			out = append(out, children...)
		case !childIDs[doc.Layers[i].ID]:
			out = append(out, doc.Layers[i])
		}
	}
	candidate := *doc
	candidate.Layers = out
	switch {
	case len(children) > 0:
		candidate.ActiveLayerID = idPtr(children[0].ID)
	case len(out) > 0:
		at := index
		if at > len(out)-1 {
			at = len(out) - 1
		}
		candidate.ActiveLayerID = idPtr(out[at].ID)
	default:
		candidate.ActiveLayerID = nil
	}
	releaseDetachedClipping(&candidate)
	if err := validateClipGraph(&candidate); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	sess.doc.Layers = out
	sess.doc.ActiveLayerID = candidate.ActiveLayerID
	return nil
}

// ---------------------------------------------------------------------------
// setClippingMask — ⌥⌘G (LiveLayerMask.swift:23-71, layerOps.ts clipping section)

type setClippingMaskPayload struct {
	ID   string `json:"id"`
	Clip *bool  `json:"clip"`
}

func planSetClippingMask(payload string) (string, func(*session) error, error) {
	var p setClippingMaskPayload
	if err := decodeLayerPayload(payload, &p); err != nil {
		return "", nil, err
	}
	if p.ID == "" {
		return "", nil, fmt.Errorf("setClippingMask 缺少 id")
	}
	if p.Clip == nil {
		return "", nil, fmt.Errorf("setClippingMask 缺少 clip")
	}
	if *p.Clip {
		return "Create Clipping Mask", func(sess *session) error {
			return linkClippingMask(sess, p.ID)
		}, nil
	}
	return "Release Clipping Mask", func(sess *session) error {
		return releaseClippingMask(sess, p.ID)
	}, nil
}

// linkClippingMask ports Swift linkMask/canLinkMask (LiveLayerMask.swift:23-38)
// as layerOps.ts toggleClippingMask's create arm: the target clips to the
// contiguous sibling below it, sharing that sibling's own base when it is
// already clipped.
func linkClippingMask(sess *session, targetID string) error {
	doc := sess.doc
	index := findLayerIndex(doc, targetID)
	if index < 0 {
		return fmt.Errorf("图层不存在: %s", targetID)
	}
	if doc.Layers[index].IsGroupLayer() {
		return fmt.Errorf("编组 %s 不能建立剪贴蒙版", targetID)
	}
	siblings := siblingIndexes(doc, parentIDOf(&doc.Layers[index]))
	position := -1
	for i, idx := range siblings {
		if idx == index {
			position = i
			break
		}
	}
	if position <= 0 {
		return fmt.Errorf("最底层图层 %s 下方没有同级图层，不能建立剪贴蒙版", targetID)
	}
	below := &doc.Layers[siblings[position-1]]
	if below.IsGroupLayer() {
		return fmt.Errorf("下方紧邻图层 %s 是编组，不能作为剪贴蒙版来源", below.ID)
	}
	source := below.ID
	if below.MaskSourceID != nil {
		source = *below.MaskSourceID
	}
	sourceLayer := layerAt(doc, source)
	if sourceLayer == nil {
		return fmt.Errorf("剪贴蒙版来源 %s 不存在", source)
	}
	if sourceLayer.Adjustment != nil {
		return fmt.Errorf("调整图层 %s 不能作为剪贴蒙版来源", source)
	}
	if pointsTo(doc.Layers[index].MaskSourceID, source) {
		return nil // already clipped through that base: nothing to record
	}
	candidate := cloneDocForEdit(doc)
	candidate.Layers[index].MaskSourceID = idPtr(source)
	if err := validateClipGraph(candidate); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	sess.doc.Layers = candidate.Layers
	return nil
}

// releaseClippingMask ports Swift removeLiveMask (LiveLayerMask.swift:53-71):
// releasing a base releases its clipped children above it that share that base;
// releasing a child leaves lower siblings untouched.
func releaseClippingMask(sess *session, targetID string) error {
	doc := sess.doc
	index := findLayerIndex(doc, targetID)
	if index < 0 {
		return fmt.Errorf("图层不存在: %s", targetID)
	}
	if doc.Layers[index].MaskSourceID == nil {
		return fmt.Errorf("图层 %s 没有剪贴蒙版，无法解除", targetID)
	}
	source := *doc.Layers[index].MaskSourceID
	candidate := cloneDocForEdit(doc)
	siblings := siblingIndexes(candidate, parentIDOf(&candidate.Layers[index]))
	position := -1
	for i, idx := range siblings {
		if idx == index {
			position = i
			break
		}
	}
	for i := position; i >= 0 && i < len(siblings); i++ {
		layer := &candidate.Layers[siblings[i]]
		if layer.ID != targetID && !pointsTo(layer.MaskSourceID, source) {
			break
		}
		layer.MaskSourceID = nil
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	sess.doc.Layers = candidate.Layers
	return nil
}

// ---------------------------------------------------------------------------
// deleteLayerMask (LayerMask.swift:287) / deleteEffect (LayerEffects.swift:182, :357)

type deleteLayerMaskPayload struct {
	ID string `json:"id"`
}

func planDeleteLayerMask(payload string) (string, func(*session) error, error) {
	var p deleteLayerMaskPayload
	if err := decodeLayerPayload(payload, &p); err != nil {
		return "", nil, err
	}
	if p.ID == "" {
		return "", nil, fmt.Errorf("deleteLayerMask 缺少 id")
	}
	return "Delete Layer Mask", func(sess *session) error {
		layer, err := layerByID(sess.doc, p.ID)
		if err != nil {
			return err
		}
		if layer.MaskFile == nil {
			return fmt.Errorf("图层 %s 没有蒙版", p.ID)
		}
		// The mask, its placement and its link state all go; MaskSourceID is
		// the clipping mask and is not part of this (layerOps.ts deleteLayerMask).
		layer.MaskFile = nil
		layer.MaskEnabled = nil
		layer.MaskLinked = nil
		layer.MaskPlacement = nil
		return nil
	}, nil
}

// effectTitles maps the wire kind to the menu title layerOps.ts
// EFFECT_KIND_TITLES / Swift LayerEffectKind.rawValue spell.
var effectTitles = map[string]string{
	"stroke":       "Stroke",
	"shadow":       "Drop Shadow",
	"colorOverlay": "Color Overlay",
	"innerShadow":  "Inner Shadow",
	"outerGlow":    "Outer Glow",
	"innerGlow":    "Inner Glow",
}

type deleteEffectPayload struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

func planDeleteEffect(payload string) (string, func(*session) error, error) {
	var p deleteEffectPayload
	if err := decodeLayerPayload(payload, &p); err != nil {
		return "", nil, err
	}
	if p.ID == "" {
		return "", nil, fmt.Errorf("deleteEffect 缺少 id")
	}
	title, ok := effectTitles[p.Kind]
	if !ok {
		return "", nil, fmt.Errorf("未知特效 kind %q", p.Kind)
	}
	return "Remove " + title, func(sess *session) error {
		layer, err := layerByID(sess.doc, p.ID)
		if err != nil {
			return err
		}
		if !effectPresent(layer.Effects, p.Kind) {
			return fmt.Errorf("图层 %s 没有%s特效", p.ID, title)
		}
		clearEffect(layer.Effects, p.Kind)
		if effectsEmpty(layer.Effects) {
			layer.Effects = nil // the last one takes the record with it
		}
		return nil
	}, nil
}

// effectPresent reports whether the effect bundle carries that kind
// (LayerEffects.swift:141-150).
func effectPresent(e *domain.Effects, kind string) bool {
	if e == nil {
		return false
	}
	switch kind {
	case "stroke":
		return e.Stroke != nil
	case "shadow":
		return e.Shadow != nil
	case "colorOverlay":
		return e.ColorOverlay != nil
	case "innerShadow":
		return e.InnerShadow != nil
	case "outerGlow":
		return e.OuterGlow != nil
	case "innerGlow":
		return e.InnerGlow != nil
	}
	return false
}

// clearEffect nils one effect field (LayerEffects.remove, LayerEffects.swift:182-191).
func clearEffect(e *domain.Effects, kind string) {
	switch kind {
	case "stroke":
		e.Stroke = nil
	case "shadow":
		e.Shadow = nil
	case "colorOverlay":
		e.ColorOverlay = nil
	case "innerShadow":
		e.InnerShadow = nil
	case "outerGlow":
		e.OuterGlow = nil
	case "innerGlow":
		e.InnerGlow = nil
	}
}

// effectsEmpty reports whether nothing is left of the bundle
// (LayerEffects.isEmpty, LayerEffects.swift:134).
func effectsEmpty(e *domain.Effects) bool {
	return e == nil || (e.Stroke == nil && e.Shadow == nil && e.ColorOverlay == nil &&
		e.InnerShadow == nil && e.OuterGlow == nil && e.InnerGlow == nil)
}

// ---------------------------------------------------------------------------
// nudgeLayer — arrow keys, 1 px or 10 px with Shift (EditorSession.swift:537)

type nudgeLayerPayload struct {
	ID string   `json:"id"`
	DX *float64 `json:"dx"`
	DY *float64 `json:"dy"`
}

func planNudgeLayer(payload string) (string, func(*session) error, error) {
	var p nudgeLayerPayload
	if err := decodeLayerPayload(payload, &p); err != nil {
		return "", nil, err
	}
	if p.ID == "" {
		return "", nil, fmt.Errorf("nudgeLayer 缺少 id")
	}
	if p.DX == nil || p.DY == nil {
		return "", nil, fmt.Errorf("nudgeLayer 缺少 dx/dy")
	}
	dx, dy := *p.DX, *p.DY
	for _, delta := range []float64{dx, dy} {
		if math.IsNaN(delta) || math.IsInf(delta, 0) {
			return "", nil, fmt.Errorf("nudgeLayer 的偏移必须是有限数值")
		}
		if math.Abs(delta) > maxNudgeDelta {
			return "", nil, fmt.Errorf("nudgeLayer 偏移 %v 超出 ±%d px", delta, maxNudgeDelta)
		}
	}
	return "Transform Layer", func(sess *session) error {
		layer, err := layerByID(sess.doc, p.ID)
		if err != nil {
			return err
		}
		moved := layer.Transform
		moved.Origin[0] += dx
		moved.Origin[1] += dy
		if !validLayerTransform(moved) {
			return fmt.Errorf("平移后图层 %s 的变换超出范围（原点 ±%d px）", p.ID, maxTransformOrigin)
		}
		layer.Transform = moved
		return nil
	}, nil
}

// ---------------------------------------------------------------------------
// mergeLayers — ⌘E (LayerMerge.swift:7 mergePlan, :34 mergeLayers)

type mergeLayersPayload struct {
	ID  string   `json:"id"`
	IDs []string `json:"ids"`
}

// mergePlan is what ⌘E merges: the participating layers bottom→top, everything
// that goes, and where the result lands.
type mergePlan struct {
	ids      []string
	removed  map[string]bool
	name     string
	parentID *string
	anchor   string
	action   string
}

func planMergeLayers(payload string) (string, func(*session) error, error) {
	var p mergeLayersPayload
	if err := decodeLayerPayload(payload, &p); err != nil {
		return "", nil, err
	}
	// "Merge Down" is mergeTitle's fallback when there is no plan
	// (LayerMerge.swift:30); the mutator renames the transaction to the action
	// the document actually picked.
	return "Merge Down", func(sess *session) error {
		return mergeLayersInSession(sess, p)
	}, nil
}

// mergePlanFor ports Swift mergePlan (LayerMerge.swift:7-27) / layerOps.ts
// mergePlan: a multi-selection merges the selected branches together (with
// anything their folders hold), a folder merges its contents and goes, and one
// layer merges with the layer beneath it in the same folder.
func mergePlanFor(doc *domain.Document, anchor string, selected []string) (*mergePlan, error) {
	if len(selected) > 1 {
		picked := map[string]bool{}
		chosen := map[string]bool{}
		for _, id := range selected {
			chosen[id] = true
			picked[id] = true
			for descendant := range descendantIDsOf(doc, id) {
				picked[descendant] = true
			}
		}
		var ordered []string
		hasPixels := false
		for i := range doc.Layers {
			if picked[doc.Layers[i].ID] {
				ordered = append(ordered, doc.Layers[i].ID)
				if !doc.Layers[i].IsGroupLayer() {
					hasPixels = true
				}
			}
		}
		if !hasPixels {
			return nil, fmt.Errorf("选中的图层没有可合并的像素图层")
		}
		top := ""
		for i := len(ordered) - 1; i >= 0; i-- {
			if chosen[ordered[i]] {
				top = ordered[i]
				break
			}
		}
		topLayer := layerAt(doc, top)
		if topLayer == nil {
			return nil, fmt.Errorf("选中的图层没有可合并的像素图层")
		}
		return &mergePlan{
			ids:      ordered,
			removed:  picked,
			name:     topLayer.Name,
			parentID: cloneIDPtr(topLayer.ParentID),
			anchor:   top,
			action:   "Merge Layers",
		}, nil
	}

	anchorIndex := findLayerIndex(doc, anchor)
	if anchorIndex < 0 {
		return nil, fmt.Errorf("图层不存在: %s", anchor)
	}
	active := &doc.Layers[anchorIndex]
	if active.IsGroupLayer() {
		inside := descendantIDsOf(doc, active.ID)
		hasPixels := false
		for id := range inside {
			if layer := layerAt(doc, id); layer != nil && !layer.IsGroupLayer() {
				hasPixels = true
				break
			}
		}
		if !hasPixels {
			return nil, fmt.Errorf("编组 %s 没有可合并的内容", active.Name)
		}
		var ids []string
		removed := map[string]bool{}
		for i := range doc.Layers {
			if inside[doc.Layers[i].ID] || doc.Layers[i].ID == active.ID {
				ids = append(ids, doc.Layers[i].ID)
				removed[doc.Layers[i].ID] = true
			}
		}
		return &mergePlan{
			ids:      ids,
			removed:  removed,
			name:     active.Name,
			parentID: cloneIDPtr(active.ParentID),
			anchor:   active.ID,
			action:   "Merge Group",
		}, nil
	}
	parent := parentIDOf(active)
	below := ""
	for i := anchorIndex - 1; i >= 0; i-- {
		if parentIDOf(&doc.Layers[i]) == parent {
			below = doc.Layers[i].ID
			break
		}
	}
	if below == "" {
		return nil, fmt.Errorf("图层 %s 下方没有同级图层，无法向下合并", anchor)
	}
	belowLayer := layerAt(doc, below)
	if belowLayer.IsGroupLayer() {
		return nil, fmt.Errorf("下方紧邻图层 %s 是编组，无法向下合并", belowLayer.Name)
	}
	return &mergePlan{
		ids:      []string{below, active.ID},
		removed:  map[string]bool{below: true, active.ID: true},
		name:     belowLayer.Name,
		parentID: cloneIDPtr(active.ParentID),
		anchor:   active.ID,
		action:   "Merge Down",
	}, nil
}

// mergeLayersInSession ports Swift mergeLayers (LayerMerge.swift:34-71): the
// participating layers composite as the canvas shows them — blend modes,
// opacity, masks, clipping and adjustments baked in — into one pixel layer,
// trimmed to what is there, in their place, as one undo step. Layers clipped to
// anything merged are re-pointed at the result (:61). An empty or failed
// composite changes nothing.
func mergeLayersInSession(sess *session, p mergeLayersPayload) error {
	doc := sess.doc
	anchor := p.ID
	if anchor == "" && len(p.IDs) == 1 {
		anchor = p.IDs[0]
	}
	if anchor == "" {
		anchor = derefString(doc.ActiveLayerID)
	}
	if anchor == "" {
		return fmt.Errorf("没有活动图层，无法合并")
	}
	var selected []string
	if len(p.IDs) > 1 {
		selected = p.IDs
	}
	plan, err := mergePlanFor(doc, anchor, selected)
	if err != nil {
		return err
	}
	// A Merge Layers selection can name a top whose own folder is part of the
	// merge (a folder and its child both selected): the original builds the same
	// invalid plan and LayerHierarchy.validate rejects it, so reject it here too.
	if plan.parentID != nil && plan.removed[*plan.parentID] {
		return fmt.Errorf("结果层的父级也在被合并的图层里，无法确定合并结果的归属")
	}

	// Only the merged layers, cut loose from anything outside the merge
	// (LayerMerge.swift:39-45).
	kept := plan.removed
	subset := make([]domain.Layer, 0, len(plan.ids))
	for i := range doc.Layers {
		if !kept[doc.Layers[i].ID] {
			continue
		}
		layer := cloneLayerRecord(doc.Layers[i])
		if layer.ParentID != nil && !kept[*layer.ParentID] {
			layer.ParentID = nil
		}
		if layer.MaskSourceID != nil && !kept[*layer.MaskSourceID] {
			layer.MaskSourceID = nil
		}
		subset = append(subset, layer)
	}
	flat := &domain.Document{Width: doc.Width, Height: doc.Height, Layers: subset}
	raster, err := render.Render(flat, sess.pixelSource())
	if err != nil {
		return fmt.Errorf("合并渲染失败: %w", err)
	}
	// Swift trims the result to its content box (PixelFilter.trimmed,
	// LayerMerge.swift:52); Go's stand-in is render.AlphaBounds, and the
	// trimmed origin keeps the pixels where the canvas showed them.
	bounds := render.AlphaBounds(raster)
	width, height := bounds[2]-bounds[0], bounds[3]-bounds[1]
	if width < 1 || height < 1 {
		return fmt.Errorf("合并结果为空，没有可合并的像素")
	}
	merged := cropBitmap(raster, bounds) // filter.go's crop, [left, top, right, bottom)

	result := newPixelLayer(plan.name, width, height)
	result.ParentID = cloneIDPtr(plan.parentID)
	result.Transform = domain.Transform{
		Origin:   [2]float64{float64(bounds[0]), float64(bounds[1])},
		Size:     [2]float64{float64(width), float64(height)},
		Sampling: domain.SamplingNearest,
	}
	slot := findLayerIndex(doc, plan.anchor)
	if slot < 0 {
		slot = len(doc.Layers)
	}
	removedBefore := 0
	for i := 0; i < slot; i++ {
		if kept[doc.Layers[i].ID] {
			removedBefore++
		}
	}
	next := make([]domain.Layer, 0, len(doc.Layers))
	for i := range doc.Layers {
		if kept[doc.Layers[i].ID] {
			continue
		}
		layer := doc.Layers[i]
		// Layers clipped to anything that was merged now clip to the result.
		if layer.MaskSourceID != nil && kept[*layer.MaskSourceID] {
			layer.MaskSourceID = idPtr(result.ID)
		}
		next = append(next, layer)
	}
	insertion := slot - removedBefore
	if insertion < 0 {
		insertion = 0
	}
	if insertion > len(next) {
		insertion = len(next)
	}
	out := make([]domain.Layer, 0, len(next)+1)
	out = append(out, next[:insertion]...)
	out = append(out, result)
	out = append(out, next[insertion:]...)

	candidate := *doc
	candidate.Layers = out
	candidate.ActiveLayerID = idPtr(result.ID)
	if err := validateClipGraph(&candidate); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	sess.renamePendingEdit(plan.action)
	sess.doc.Layers = out
	sess.doc.ActiveLayerID = idPtr(result.ID)
	sess.bitmaps[*result.ImageFile] = merged
	return nil
}

// renamePendingEdit renames the history transaction Workspace.EditActive
// opened, so an op whose undo name comes from the plan the document decides
// (⌘E: Merge Down / Merge Group / Merge Layers, LayerMerge.swift:16/22/26)
// records that action. The outer transaction closes while the document is
// still untouched — History.End records nothing for an unchanged document and
// leaves the redo stack alone — then reopens under the new name.
func (sess *session) renamePendingEdit(name string) {
	sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
}
