// flutterkill site: an interactive replica of the TUI, plus small page helpers.
// Plain JS, no dependencies, no network. Projects, sizes, ages and pins come
// from sample.js, which site/build.sh generates by running
// `flutterkill scan --json` on demo/setup.sh's synthetic HOME: dummy data,
// never a real machine. Removals are acted out here; nothing is removed.
(() => {
  "use strict";
  document.documentElement.classList.add("js");
  const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];

  /* ---------- copy buttons ---------- */
  const copy = async (text) => {
    try { await navigator.clipboard.writeText(text); }
    catch {
      const t = Object.assign(document.createElement("textarea"), { value: text });
      document.body.append(t); t.select(); document.execCommand("copy"); t.remove();
    }
  };
  $$("[data-copy]").forEach((b) => b.addEventListener("click", async () => {
    await copy(b.dataset.copy);
    b.classList.add("done");
    clearTimeout(b._t); b._t = setTimeout(() => b.classList.remove("done"), 1600);
  }));

  /* ---------- install tabs (ARIA tabs pattern) ---------- */
  const tabs = $$('[role="tab"]');
  const pick = (t) => {
    tabs.forEach((x) => {
      const on = x === t;
      x.setAttribute("aria-selected", on); x.tabIndex = on ? 0 : -1;
      $("#" + x.getAttribute("aria-controls")).hidden = !on;
    });
    t.focus();
  };
  tabs.forEach((t, i) => {
    t.addEventListener("click", () => pick(t));
    t.addEventListener("keydown", (e) => {
      const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
      if (d) { e.preventDefault(); pick(tabs[(i + d + tabs.length) % tabs.length]); }
    });
  });

  /* ---------- reveal on scroll ---------- */
  if (!reduceMotion && "IntersectionObserver" in window) {
    const io = new IntersectionObserver((es) => es.forEach((e) => {
      if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
    }), { rootMargin: "0px 0px -8% 0px" });
    $$(".sec h2, .sec .intro, .arts, .sec .two > .window, .cat, .pins-wrap .window, .safety article, .checks li, .kid, .usage, .listing, .keytable, .tscroll, .recording, .platforms div").forEach((el, i) => {
      el.classList.add("reveal"); el.style.transitionDelay = (i % 4) * 60 + "ms"; io.observe(el);
    });
  }

  /* ---------- recording: video, GIF fallback ---------- */
  const video = $("#demo-video"), play = $("#play");
  if (video) {
    const toGif = () => {
      const img = Object.assign(new Image(1280, 640), { src: "demo.gif", alt: video.getAttribute("aria-label"), loading: "lazy" });
      video.replaceWith(img); play.hidden = true;
    };
    const srcs = $$("source", video);
    srcs[srcs.length - 1].addEventListener("error", toGif);
    const start = (manual) => { play.hidden = true; if (manual === true) video.controls = true; video.play().catch(() => { play.hidden = false; }); };
    play.addEventListener("click", () => start(true));
    video.addEventListener("click", () => { if (!video.controls) { if (video.paused) video.play(); else video.pause(); } });
    const saveData = navigator.connection && navigator.connection.saveData;
    if (!reduceMotion && !saveData && "IntersectionObserver" in window) {
      const vio = new IntersectionObserver((es) => es.forEach((e) => {
        if (e.isIntersecting) start(); else if (!video.paused) video.pause();
      }), { threshold: 0.5 });
      vio.observe(video);
    }
  }

  /* =====================================================================
     Data: flutterkill scan --json on the demo workspace
     ===================================================================== */
  const term = $("#term");
  const DATA = window.FLUTTERKILL_SAMPLE;
  if (!DATA || !DATA.scan) {
    if (term) term.innerHTML = '<img src="demo.gif" alt="flutterkill on four synthetic Flutter projects" style="width:100%;height:auto">';
    return;
  }
  const S = DATA.scan;
  const NOW = Date.parse(S.generated_at) || Date.now(); // ages stay as they were at build time
  const ROOT = (S.roots && S.roots[0]) || "~/dev";
  const projects = [], caches = [], byPath = {};
  S.items.forEach((it) => {
    const bytes = it.measured === false ? -1 : (it.bytes && it.bytes.allocated) || 0;
    if (it.category === "project" && it.project) {
      let p = byPath[it.project.path];
      if (!p) {
        const la = it.project.last_activity || {};
        p = byPath[it.project.path] = { name: it.project.name, path: it.project.path, fvm: it.project.fvm_version || "", at: Date.parse(la.at) || 0, src: la.source || "", arts: [] };
        projects.push(p);
      }
      p.arts.push({ name: it.name, path: it.path, bytes, key: it.path, action: it.action });
    } else {
      caches.push({ id: it.id, kind: it.kind, name: it.name, path: it.path, bytes, note: it.explain || "", usedBy: it.used_by || [], command: it.command || [], action: it.action, safe: it.safety && it.safety.level === "safe", daemon: !!(it.safety && it.safety.reasons.some((r) => r.code === "daemon_recent")), key: "cache:" + it.id });
    }
  });

  /* ---------- formatting, as internal/format ---------- */
  const fmtBytes = (n) => {
    if (n < 0) return "…";
    if (n < 1000) return n + " B";
    let div = 1000, exp = 0;
    for (let m = Math.floor(n / 1000); m >= 1000; m = Math.floor(m / 1000)) { div *= 1000; exp++; }
    return (n / div).toFixed(1) + " " + "kMGTPE"[exp] + "B";
  };
  const DAY = 86400e3;
  const ago = (t) => {
    if (!t) return "?";
    const days = Math.floor((NOW - t) / DAY);
    if (days < 1) return "today";
    if (days < 14) return days + "d ago";
    if (days < 60) return Math.floor(days / 7) + "w ago";
    if (days < 365) return Math.floor(days / 30) + "mo ago";
    return (days / 365).toFixed(1) + "y ago";
  };
  const rel = (path) => (path.startsWith(ROOT + "/") ? path.slice(ROOT.length + 1) : path.split("/").pop());
  const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
  const padR = (s, n) => s + " ".repeat(Math.max(0, n - [...s].length));
  const padL = (s, n) => " ".repeat(Math.max(0, n - [...s].length)) + s;

  /* ---------- the pins panel in the caches section ---------- */
  const pinsEl = $("#pins");
  const pinned = ["fvm", "gradle-dist", "gradle-version"].flatMap((k) => caches.filter((c) => c.kind === k).sort((a, b) => b.bytes - a.bytes));
  if (pinsEl && pinned.length) {
    pinsEl.innerHTML = pinned.map((c) => `<div class="pin${c.usedBy.length ? "" : " free"}"><b>${esc(c.name)}</b><span class="sz">${fmtBytes(c.bytes)}</span><span class="by">${esc(c.usedBy.length ? c.usedBy.join(", ") : "nothing scanned uses it")}</span></div>`).join("");
  }

  /* =====================================================================
     The TUI replica (internal/ui: model.go and view.go)
     ===================================================================== */
  if (!term) return;
  const keysEl = $("#term-keys"), hint = $("#try-hint");
  const IDLE = 0, QUEUED = 1, WORKING = 2, DONE = 3;
  const SORTS = ["size", "oldest", "name"];
  const BAR = 14, BAR_MIN = 90, DETAIL = 4, CHROME = 4 + 1 + DETAIL + 2;
  const STALE = 180 * DAY, ACTIVE = 30 * DAY;
  const NOTES = { // artifactNotes in internal/ui/view.go
    "build": "Build output. Recreated by the next `flutter build` / `flutter run`.",
    ".dart_tool": "Pub and build_runner state. `flutter pub get` (run automatically by flutter run) restores it.",
    "ios/Pods": "Installed CocoaPods. `pod install` (run by flutter build ios) restores it.",
    "ios/.symlinks": "Plugin symlinks for CocoaPods. Recreated by the next iOS build.",
    "macos/Pods": "Installed CocoaPods. Restored by the next macOS build.",
    "android/.gradle": "Project-level Gradle cache. Recreated by the next Android build.",
    "android/app/.cxx": "Native (CMake/NDK) build intermediates. Recreated by the next Android build.",
    "ios/Flutter/ephemeral": "Generated Flutter files for Xcode. Recreated by the next iOS build.",
    "macos/Flutter/ephemeral": "Generated Flutter files and plugin symlinks. Recreated by the next macOS build.",
    "linux/flutter/ephemeral": "Flutter engine copy and plugin symlinks. Recreated by the next Linux build.",
    "windows/flutter/ephemeral": "Flutter engine copy and plugin symlinks. Recreated by the next Windows build.",
  };

  let st, cols = 120, lines = 24, timer = 0;
  const reset = () => {
    clearTimeout(timer);
    st = { tab: projects.length ? 0 : 1, sort: 0, cursorKey: "", offset: 0, expanded: {}, selected: {}, status: {}, confirm: null, queue: [], legend: false, quitWarn: false, outcomes: [], exited: false, touched: {} };
    // Like the TUI, the cursor is pinned before sizes arrive: on the first
    // project in scan order, not the biggest.
    const r = rows();
    if (st.tab === 0 && projects.length) st.cursorKey = "project:" + projects[0].path;
    else if (r.length) st.cursorKey = key(r[0]);
  };

  /* ---------- rows ---------- */
  const key = (r) => (r.c ? r.c.key : r.art >= 0 ? r.p.arts[r.art].key : "project:" + r.p.path);
  const leaves = (r) => (r.p && r.art < 0 ? r.p.arts.map((a) => a.key) : [key(r)]);
  const pending = (p) => p.arts.reduce((n, a) => n + (st.status[a.key] !== DONE && a.bytes > 0 ? a.bytes : 0), 0);
  const rows = () => {
    if (st.tab === 1) {
      return caches.slice().sort((a, b) => b.bytes - a.bytes)
        .filter((c) => !(c.bytes === 0 && (st.status[c.key] || IDLE) === IDLE)).map((c) => ({ c, art: -1 }));
    }
    const ps = projects.slice().sort((a, b) =>
      st.sort === 1 ? a.at - b.at : st.sort === 2 ? (a.name.toLowerCase() < b.name.toLowerCase() ? -1 : a.name.toLowerCase() > b.name.toLowerCase() ? 1 : 0) : pending(b) - pending(a));
    const out = [];
    ps.forEach((p) => {
      out.push({ p, art: -1 });
      if (st.expanded[p.path]) p.arts.forEach((_, i) => out.push({ p, art: i }));
    });
    return out;
  };
  const cursor = (rs) => Math.max(0, rs.findIndex((r) => key(r) === st.cursorKey));
  const rowBytes = (r) => (r.c ? r.c.bytes : r.art >= 0 ? r.p.arts[r.art].bytes : pending(r.p));
  const lookup = (k) => {
    for (const c of caches) if (c.key === k) return { c };
    for (const p of projects) for (const a of p.arts) if (a.key === k) return { p, a };
    return {};
  };
  const size = (k) => { const { c, a } = lookup(k); return Math.max(0, c ? c.bytes : a ? a.bytes : 0); };
  const sum = (keys) => keys.reduce((n, k) => n + size(k), 0);
  const totals = () => [projects.reduce((n, p) => n + pending(p), 0), caches.reduce((n, c) => n + (st.status[c.key] !== DONE ? Math.max(c.bytes, 0) : 0), 0)];
  const locked = (k) => [QUEUED, WORKING, DONE].includes(st.status[k]);
  const busy = () => st.queue.length > 0 || Object.values(st.status).includes(WORKING);
  const selectedHere = (rs) => rs.filter((r) => r.art < 0).flatMap(leaves).filter((k) => st.selected[k]);
  const listHeight = () => Math.max(1, lines - CHROME);

  /* ---------- styled segments, truncated like ansi.Truncate ---------- */
  // A line is a list of [class, text] segments; it is cut to the terminal
  // width with "…", so narrow screens lose the end of a line, as in a terminal.
  const fit = (segs) => {
    let left = cols, html = "";
    const total = segs.reduce((n, [, t]) => n + [...t].length, 0);
    const cut = total > cols;
    if (cut) left = cols - 1;
    for (const [c, t, attr] of segs) {
      if (left <= 0) break;
      const chars = [...t], s = chars.slice(0, left).join("");
      left -= Math.min(left, chars.length);
      html += c || attr ? `<span${c ? ` class="${c}"` : ""}${attr || ""}>${esc(s)}</span>` : esc(s);
    }
    return cut ? html + "…" : html;
  };
  const ln = (segs = [], attr = "", cls = "ln") => `<span class="${cls}"${attr}>${fit(segs) || " "}</span>`;
  const faintAll = (segs, c) => segs.map(([, t, a]) => [c, t, a]);

  const measure = () => {
    const probe = document.createElement("span");
    probe.textContent = "0".repeat(100);
    probe.style.cssText = "position:absolute;visibility:hidden;white-space:pre";
    term.append(probe);
    const cw = probe.getBoundingClientRect().width / 100;
    probe.remove();
    const cs = getComputedStyle(term);
    const lh = parseFloat(cs.lineHeight) || 18;
    cols = Math.max(36, Math.floor((term.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight)) / cw));
    lines = Math.max(16, Math.round((term.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom)) / lh));
  };

  /* ---------- pieces of the screen ---------- */
  const SEP = ["f", "  ·  "];
  const header = () => {
    const [pb, cb] = totals();
    return [[" ", " "], ["brandb", " flutterkill "], ["", " "], ["g", "● trash"], SEP, ["c", fmtBytes(pb + cb)], ["", " reclaimable"], SEP, ["", projects.length + " projects"], SEP, ["f", ROOT]];
  };
  const tabsLine = () => {
    const [pb, cb] = totals();
    const p = "1 Projects " + fmtBytes(pb), c = "2 Global caches " + fmtBytes(cb);
    return [["", "  "], [st.tab === 0 ? "h" : "f", p, ' data-tab="0"'], ["", "    "], [st.tab === 1 ? "h" : "f", c, ' data-tab="1"']];
  };
  const showBar = () => cols >= BAR_MIN;
  const columns = () => {
    const bar = showBar() ? " ".repeat(BAR + 2) : "";
    if (st.tab === 1) return [["f", "    " + bar + padL("SIZE", 9) + "  " + padR("SAFETY", 10) + " CACHE"]];
    return [["f", "    " + bar + padL("SIZE", 9) + "  " + padR("ACTIVE", 8) + "  " + padR("SAFETY", 10) + " PROJECT  (sort: " + SORTS[st.sort] + ")"]];
  };
  const EIGHTHS = ["", "▏", "▎", "▍", "▌", "▋", "▊", "▉"];
  const fine = (n, total, width) => {
    if (total <= 0 || n <= 0) return "";
    const units = Math.max(1, Math.min(Math.floor((n * width * 8) / total), width * 8));
    return "█".repeat(Math.floor(units / 8)) + EIGHTHS[units % 8];
  };
  const bar = (n, largest) => {
    if (n < 0) return [["f", "·".repeat(BAR)]];
    const cells = fine(n, largest, BAR);
    return [["c", cells], ["f", "·".repeat(BAR - [...cells].length)]];
  };
  const badge = (caution) => (caution ? ["y", "▲ caution "] : ["g", "● safe    "]);
  const check = (keys) => {
    let sel = 0, fin = 0;
    for (const k of keys) {
      const s = st.status[k] || IDLE;
      if (s === DONE) fin++;
      else if (s === WORKING || s === QUEUED) return ["f", "◌"];
      else if (st.selected[k]) sel++;
    }
    if (keys.length && fin === keys.length) return ["g", "✓"];
    if (sel > 0 && sel + fin === keys.length) return ["c", "●"];
    if (sel > 0) return ["c", "◐"];
    return ["f", "○"];
  };
  const ageSeg = (t) => {
    const s = padR(ago(t), 8), d = NOW - t;
    return [!t ? "f" : d < ACTIVE ? "g" : d >= STALE ? "y" : "", s];
  };
  // dim styles a leaf line by its clean-up status: struck when done, faint while queued.
  const dim = (k, segs) => {
    const s = st.status[k] || IDLE;
    if (s === DONE) {
      const plain = segs.map(([, t]) => t).join(""), text = plain.replace(/^ +/, "");
      return [["", plain.slice(0, plain.length - text.length)], ["s", text]];
    }
    if (s === WORKING || s === QUEUED) return faintAll(segs, "f");
    return segs;
  };
  const cacheFlags = (c) => {
    const parts = c.command.length ? [["r", "no undo"], ["", " "], ["f", "runs `" + c.command.join(" ") + "`"]] : [["f", c.path]];
    if (c.usedBy.length) parts.push(["", " "], ["y", `used by ${c.usedBy.length} project(s)`]);
    return parts;
  };
  const row = (r, isCur, largest, i) => {
    const prefix = [isCur ? ["cur", "▌"] : ["", " "], ["", " "], [...check(leaves(r)), ' data-mark="1"'], ["", " "]];
    const n = rowBytes(r);
    let b = [];
    if (showBar()) {
      b = [...bar(n, largest), ["", "  "]];
      if ((r.art >= 0 || r.c) && ![IDLE, undefined].includes(st.status[key(r)])) b = [["f", "·".repeat(BAR)], ["", "  "]];
    }
    let segs;
    if (r.c) {
      segs = prefix.concat(b, dim(r.c.key, [["", padL(fmtBytes(n), 9) + "  "], badge(!r.c.safe), ["", " "], ["b", r.c.name], ["", "  "], ...cacheFlags(r.c)]));
    } else if (r.art >= 0) {
      const a = r.p.arts[r.art];
      segs = prefix.concat(b, dim(a.key, [["", padL(fmtBytes(n), 9) + "  " + " ".repeat(8) + "  " + " ".repeat(10) + "     " + a.name + "/"]]));
    } else {
      const p = r.p;
      segs = prefix.concat(b, [["", padL(fmtBytes(n), 9) + "  "], ageSeg(p.at), ["", "  "], badge(false), ["", " "], ["f", st.expanded[p.path] ? "▾" : "▸"], ["", " "], ["b", p.name]]);
      if (rel(p.path) !== p.name) segs.push(["", "  "], ["f", rel(p.path)]);
      if (p.fvm) segs.push(["", "  "], ["f", "fvm " + p.fvm]);
    }
    return ln(segs, ` data-row="${i}"`, "row ln" + (isCur ? " on" : ""));
  };
  const cacheWhy = (c) => {
    if (c.safe) return "no scanned project uses this Gradle version";
    const why = ["shared by every project; the next build is slower"];
    if (c.daemon) why.unshift("a Gradle daemon of this version ran in the last 3 hours");
    if (c.usedBy.length) why.unshift(`used by ${c.usedBy.length} scanned project(s)`);
    if (c.command.length) why.unshift("its own clean-up command deletes (no undo)");
    return why.join(" · ");
  };
  const detail = (r) => {
    const out = [];
    if (r.c) {
      const c = r.c;
      out.push([["", "  "], ["b", c.name], ["", "  "], c.safe ? ["g", "safe"] : ["y", "caution"], ["", "  "], ["f", cacheWhy(c)]]);
      out.push(c.command.length ? [["", "  "], ["r", "Runs `" + c.command.join(" ") + "`, the tool's own clean-up. Not recoverable from the Trash."]] : [["", "  "], ["f", c.path]]);
      out.push([["", "  " + c.note]]);
    } else if (r.art >= 0) {
      const a = r.p.arts[r.art];
      out.push([["", "  "], ["b", r.p.name + "/" + a.name], ["", "  "], ["g", "safe"]], [["", "  "], ["f", a.path]], [["", "  " + (NOTES[a.name] || "")]]);
    } else {
      const p = r.p;
      const src = { git: "last commit", mtime: "last file change" }[p.src] || "activity unknown";
      let info = src + " " + ago(p.at);
      if (p.fvm) info = "fvm " + p.fvm + " · " + info;
      out.push([["", "  "], ["b", p.name], ["", "  "], ["f", info]], [["", "  "], ["f", p.path]]);
      const arts = p.arts.filter((a) => st.status[a.key] !== DONE).map((a) => a.name + " " + fmtBytes(a.bytes));
      if (arts.length) out.push([["", "  "], ["c", "rebuildable: "], ["", arts.join(", ")]]);
    }
    return out;
  };
  const tally = () => {
    const t = { trashed: 0, delegated: 0, n: 0 };
    st.outcomes.forEach((o) => { t[o.how] += o.bytes; t.n++; });
    return t;
  };
  const NOT_MEASURED = "free space not measured in this replica";
  const summary = () => {
    const sel = selectedHere(rows());
    const parts = [[["", `${sel.length} selected (${fmtBytes(sum(sel))})`]]];
    const t = tally();
    if (st.outcomes.some((o) => o.how === "trashed")) parts.push([["g", "Moved to Trash: " + fmtBytes(t.trashed)]], [["f", NOT_MEASURED + " (empty the Trash to reclaim)"]]);
    if (st.outcomes.some((o) => o.how === "delegated")) parts.push([["g", "Delegated: " + fmtBytes(t.delegated)]], [["f", NOT_MEASURED]]);
    if (st.queue.length) parts.push([["f", `${st.queue.length} queued`]]);
    return [["", "  "]].concat(...parts.map((p, i) => (i ? [SEP, ...p] : p)));
  };
  const help = () => {
    if (st.legend) return [["f", "  any key closes"]];
    if (st.confirm) return [["y", "  Proceed?"], ["", "  "], ["b", "y"], ["f", " yes   any other key cancels"]];
    if (st.quitWarn && busy()) return [["y", "  Stopping after the current item. Press ctrl+c again to quit now, or wait."]];
    if (st.tab === 1) return [["f", "  ↑↓ move  space select  a all  d delete  tab projects  ? legend  q quit"]];
    return [["f", "  ↑↓ move  space select  a all  →← expand  d delete  tab caches  o sort  ? legend  q quit"]];
  };
  const confirmLines = (height) => {
    const n = st.confirm.length;
    const out = [[["y", `  Move to the Trash: ${n} item(s), about ${fmtBytes(sum(st.confirm))}`]], []];
    for (let i = 0; i < n; i++) {
      if (out.length >= height - 1 && i < n - 1) { out.push([["f", `    … and ${n - i} more`]]); break; }
      const { c, a } = lookup(st.confirm[i]);
      if (c && c.command.length) out.push([["", "    " + padL(fmtBytes(c.bytes), 9) + "  "], ["b", c.name], ["", "  "], ["r", "runs `" + c.command.join(" ") + "`, not recoverable"]]);
      else if (c) out.push([["", "    " + padL(fmtBytes(c.bytes), 9) + "  "], ["b", c.name], ["", "  "], ["f", c.path]]);
      else if (a) out.push([["", "    " + padL(fmtBytes(a.bytes), 9) + "  " + rel(a.path)]]);
    }
    return out;
  };
  const legendLines = () => [
    [["", "  "], ["b", "Safety"]],
    [["", "    "], badge(false), ["", "  project build output, or a Gradle version no scanned project uses; moves to the Trash"]],
    [["", "    "], badge(true), ["", "  machine-wide cache: shared by every project, maybe pinned by one, or cleaned by its own command (no undo)"]],
    [],
    [["", "  "], ["b", "Marks"], ["", "  ○ not selected  ◐ partly  ● selected  ◌ queued or running  ✓ done  ✗ failed (see the detail)"]],
    [["", "  "], ["b", "Bar"], ["", "    "], ["c", "███"], ["", " size, relative to the largest item in this tab"]],
    [],
    [["", "  Only directories at a known place next to a pubspec.yaml are listed; symlinks and git-tracked files are refused."]],
    [["", "  Removals run one at a time. Freed space is measured from the disk, not assumed."]],
    [["", "  Moving to the Trash frees nothing until you empty it."]],
  ];

  /* ---------- after q: the receipt, as report.Receipt prints it ---------- */
  const receipt = () => {
    let out = ln([["f", "$ "], ["", "flutterkill"]]);
    st.outcomes.forEach((o) => {
      const what = o.name ? o.name + "  " + o.path : o.path;
      out += ln([["", padR(o.how, 13) + " " + padL(fmtBytes(o.bytes), 9) + "  " + what]]);
    });
    const t = tally();
    if (st.outcomes.some((o) => o.how === "trashed")) out += ln([["", `Moved about ${fmtBytes(t.trashed)} to the Trash (${NOT_MEASURED}). Empty the Trash to reclaim it.`]]);
    if (st.outcomes.some((o) => o.how === "delegated")) out += ln([["", `Delegated about ${fmtBytes(t.delegated)} to the tools' own clean-up commands (${NOT_MEASURED}).`]]);
    out += ln([["f", "$ "], ["cur", "█"]]) + ln();
    out += ln([["f", st.outcomes.length
      ? "  The real receipt also measures the change in free disk space before and after each item."
      : "  Nothing was removed, so there is nothing on the receipt."]]);
    out += ln([["f", "  Nothing on this page was removed. r runs flutterkill again."]]);
    return out;
  };

  /* ---------- render ---------- */
  const scroll = () => {
    const rs = rows(), cur = cursor(rs), body = listHeight();
    if (cur < st.offset) st.offset = cur;
    if (cur >= st.offset + body) st.offset = cur - body + 1;
    st.offset = Math.max(0, Math.min(st.offset, rs.length - body));
  };
  const render = () => {
    measure();
    if (st.exited) {
      term.innerHTML = `<div class="screen">${receipt()}</div>`;
      return renderKeys();
    }
    scroll();
    const rs = rows(), cur = cursor(rs), body = listHeight();
    let top = ln(header()) + ln() + ln(tabsLine());
    let list = [];
    if (st.legend) { top += ln(); list = legendLines().map((s) => ln(s)); }
    else if (st.confirm) { top += ln(); list = confirmLines(body).map((s) => ln(s)); }
    else if (!rs.length) {
      top += ln();
      list = [ln([["f", st.tab === 1 ? "  No global caches found." : "  No Dart/Flutter projects with build artifacts under " + ROOT + "."]])];
    } else {
      top += ln(columns());
      const largest = Math.max(1, ...rs.filter((r) => r.art < 0).map(rowBytes));
      for (let i = st.offset; i < Math.min(rs.length, st.offset + body); i++) list.push(row(rs[i], i === cur, largest, i));
    }
    while (list.length < body) list.push(ln());
    let foot = ln([["f", "─".repeat(cols)]]);
    const d = !st.confirm && !st.legend && rs.length ? detail(rs[cur]) : [];
    for (let i = 0; i < DETAIL; i++) foot += ln(d[i] || []);
    foot += ln(summary()) + ln(help());
    term.innerHTML = `<div class="screen">${top}${list.slice(0, body).join("")}</div><div class="tfoot">${foot}</div>`;
    renderKeys();
  };

  // The suggested next key glows, following the recorded demo's path.
  const suggest = () => {
    if (st.exited) return "r";
    if (st.confirm) return "y";
    if (st.legend) return "esc";
    const rs = rows(), r = rs[cursor(rs)];
    if (st.tab === 0) {
      if (selectedHere(rs).length) return "d";
      if (st.outcomes.length) return "tab";
      if (!st.touched.o) return "o";
      if (r && r.art < 0 && !st.expanded[r.p.path]) return "→";
      return "space";
    }
    if (selectedHere(rs).length) return "d";
    return st.touched.legend ? "q" : "?";
  };
  const KEYSETS = {
    projects: [["↑", ""], ["↓", ""], ["space", "select"], ["a", "all"], ["→", "expand"], ["←", "collapse"], ["d", "delete"], ["tab", "caches"], ["o", "sort"], ["?", "legend"], ["q", "quit"]],
    caches: [["↑", ""], ["↓", ""], ["space", "select"], ["a", "all"], ["d", "delete"], ["tab", "projects"], ["?", "legend"], ["q", "quit"]],
    confirm: [["y", "yes"], ["esc", "cancel"]],
    legend: [["esc", "close"]],
    exited: [["r", "run again"]],
  };
  const renderKeys = () => {
    const set = KEYSETS[st.exited ? "exited" : st.confirm ? "confirm" : st.legend ? "legend" : st.tab ? "caches" : "projects"];
    const s = suggest();
    keysEl.innerHTML = set.map(([k, l]) => `<button type="button" data-key="${esc(k)}" class="${k === s ? "primary" + (reduceMotion ? "" : " pulse") : ""}"><b>${esc(k)}</b>${esc(l)}</button>`).join("");
  };

  /* ---------- removals: one at a time, acted out ---------- */
  const next = () => {
    if (!st.queue.length) { st.quitWarn = false; return; }
    const k = st.queue.shift();
    st.status[k] = WORKING;
    const { c, a } = lookup(k);
    timer = setTimeout(() => {
      delete st.selected[k];
      st.status[k] = DONE;
      const cmd = c && c.action === "command";
      st.outcomes.push({ how: cmd ? "delegated" : "trashed", bytes: size(k), name: c ? c.name : "", path: c ? c.path : a.path });
      next();
      render();
    }, reduceMotion ? 120 : 520 + Math.min(900, size(k) / 1e5));
  };
  const stop = () => {
    st.queue.forEach((k) => { st.status[k] = IDLE; });
    st.queue = [];
    st.quitWarn = true;
  };
  const toggle = (keys) => {
    keys = keys.filter((k) => !locked(k));
    const all = keys.length > 0 && keys.every((k) => st.selected[k]);
    keys.forEach((k) => { if (all) delete st.selected[k]; else st.selected[k] = true; });
  };
  const quit = () => { clearTimeout(timer); st.queue = []; st.exited = true; };

  /* ---------- keys, as Model.key ---------- */
  const press = (k) => {
    if (hint) hint.classList.add("gone");
    if (st.exited) {
      if (k === "r" || k === "enter") { reset(); render(); return true; }
      return false;
    }
    if (k === "ctrl+c") {
      if (busy() && !st.quitWarn) stop(); else quit();
      return render(), true;
    }
    if (st.legend) { st.legend = false; return render(), true; }
    if (st.confirm) {
      if (k === "y" || k === "Y") {
        st.confirm.forEach((x) => { st.status[x] = QUEUED; });
        const wasBusy = busy();
        st.queue.push(...st.confirm);
        st.confirm = null;
        if (!wasBusy) next();
      } else st.confirm = null;
      return render(), true;
    }
    const rs = rows();
    let i = cursor(rs);
    switch (k) {
      case "q": case "esc":
        if (busy()) stop(); else quit();
        return render(), true;
      case "?": st.legend = true; st.touched.legend = true; return render(), true;
      case "up": i--; break;
      case "down": i++; break;
      case "pgup": i -= listHeight(); break;
      case "pgdown": i += listHeight(); break;
      case "home": case "g": i = 0; break;
      case "end": case "G": i = rs.length - 1; break;
      case "tab": case "1": case "2":
        st.tab = k === "1" ? 0 : k === "2" ? 1 : 1 - st.tab;
        st.offset = 0;
        { const r2 = rows(); st.cursorKey = r2.length ? key(r2[0]) : ""; }
        return render(), true;
      case "o": case "s": st.sort = (st.sort + 1) % 3; st.touched.o = true; return render(), true;
      case "right": case "l": case "enter":
        if (rs[i] && rs[i].p) { const p = rs[i].p.path; st.expanded[p] = k !== "enter" || !st.expanded[p]; }
        return render(), true;
      case "left": case "h":
        if (rs[i] && rs[i].p) { delete st.expanded[rs[i].p.path]; st.cursorKey = "project:" + rs[i].p.path; }
        return render(), true;
      case "space": if (rs[i]) toggle(leaves(rs[i])); return render(), true;
      case "a": toggle(rs.filter((r) => r.art < 0).flatMap(leaves)); return render(), true;
      case "d": case "x": {
        let keys = selectedHere(rs);
        if (!keys.length && rs[i]) keys = leaves(rs[i]);
        keys = keys.filter((x) => !locked(x)).sort();
        if (keys.length) st.confirm = keys;
        return render(), true;
      }
      default: return false;
    }
    if (rs.length) st.cursorKey = key(rs[Math.max(0, Math.min(i, rs.length - 1))]);
    render();
    return true;
  };

  const KEYMAP = { ArrowUp: "up", k: "up", ArrowDown: "down", j: "down", ArrowLeft: "left", ArrowRight: "right", Enter: "enter", Escape: "esc", " ": "space", Tab: "tab", PageUp: "pgup", PageDown: "pgdown", Home: "home", End: "end", Delete: "d", Backspace: "d" };
  term.addEventListener("keydown", (e) => {
    if (e.ctrlKey && (e.key === "c" || e.key === "C")) { press("ctrl+c"); e.preventDefault(); return; }
    if (e.ctrlKey && e.key === "u") { press("pgup"); e.preventDefault(); return; }
    if (e.ctrlKey && e.key === "d") { press("pgdown"); e.preventDefault(); return; }
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.key === "Tab" && e.shiftKey) return; // Shift+Tab leaves the demo
    const k = KEYMAP[e.key] || (e.key.length === 1 ? e.key : null);
    if (!k) return;
    if (press(k) !== false) e.preventDefault();
  });
  keysEl.addEventListener("click", (e) => {
    const b = e.target.closest("button[data-key]");
    if (!b) return;
    const map = { "↑": "up", "↓": "down", "→": "right", "←": "left" };
    press(map[b.dataset.key] || b.dataset.key);
  });
  term.addEventListener("click", (e) => {
    term.focus({ preventScroll: true });
    if (st.exited) return;
    if (st.legend) { press("esc"); return; }
    const t = e.target.closest("[data-tab]");
    if (t) { press(t.dataset.tab === "0" ? "1" : "2"); return; }
    const r = e.target.closest("[data-row]");
    if (!r || st.confirm) return;
    const rs = rows(), i = +r.dataset.row;
    const was = cursor(rs) === i;
    st.cursorKey = key(rs[i]);
    if (e.target.closest("[data-mark]")) press("space");
    else if (was) press(rs[i].art < 0 && !rs[i].c ? "enter" : "space");
    else render();
  });

  let rt;
  addEventListener("resize", () => { clearTimeout(rt); rt = setTimeout(render, 80); });

  reset();
  render();
})();
