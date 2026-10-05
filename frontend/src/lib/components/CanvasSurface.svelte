<script lang="ts">
  import type { DocTab } from "../state/workspace";

  let { doc }: { doc: DocTab } = $props();
</script>

<!--
  Ticket 02 renders the document at 100% inside a scrollable stage.
  The real viewport (zoom ladder, anchoring, pan) is ticket 13.
-->
<div class="canvas-scroll">
  <div
    class="doc"
    style:width="{doc.width}px"
    style:height="{doc.height}px"
  ></div>
</div>

<style>
  .canvas-scroll {
    position: absolute;
    inset: 0;
    overflow: auto;
    /* safe center: plain center clips the start edge when the document is
       larger than the stage and makes it unscrollable (review I3). */
    display: grid;
    place-items: safe center;
  }

  /* Checkerboard (transparency) + document bounds. margin:auto keeps the
     document centered in engines without `safe` alignment. */
  .doc {
    flex: none;
    margin: auto;
    background: repeating-conic-gradient(#3c3c41 0% 25%, #4a4a50 0% 50%) 0 0 / 16px 16px;
    border: 1px solid #141416;
    box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.08), 0 8px 32px rgba(0, 0, 0, 0.45);
  }
</style>
