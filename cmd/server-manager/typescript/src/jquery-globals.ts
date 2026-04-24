// Many jQuery plugins in node_modules (multiselect, bootstrap-switch,
// summernote, quicksearch, ...) end with an IIFE like `}(window.jQuery);`.
// @rollup/plugin-inject can rewrite bare `$` / `jQuery` references into real
// imports, but it does NOT touch `window.jQuery`. Under the old
// browserify-shim pipeline the shim copy of jQuery became a window global
// automatically; Vite does not.
//
// Importing this file first (before any jQuery plugin) attaches jQuery to
// window so those `}(window.jQuery)` closures find it.
import $ from "jquery";

const w = window as unknown as { $: typeof $; jQuery: typeof $ };
w.$ = $;
w.jQuery = $;
