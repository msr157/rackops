# RackOps Lite dashboard — design system

**World: "Quiet console."** A NetBird-register dark admin console: one
continuous near-black neutral surface, content sitting directly on the canvas,
hierarchy from hairlines, whitespace, and type scale — almost never from boxes.
One signal-green accent (shared with the rackops.dev marketing site) does all
brand work. Square by default; radius only on controls and floating layers.

Provenance: full visual-world replacement, user-directed 2026-08-01 with five
NetBird admin screenshots as the pinned reference ("look how good this is"),
explicitly rejecting the previous card-heavy look ("too many boxes with rounded
corners"). Accent chosen by user: the website's signal green. Density chosen by
user: **NetBird-minimal** — the 2026-07-27 "reference register" richness
(hero chart, KPI deltas, sparklines) is REMOVED, not restyled. Mode: **Operate**
on every routed page. Physical scene unchanged: an operator checking their rack
from a laptop in the evening — dark, calm, zero spectacle.

Direction contract —
THESIS: the console is one quiet dark surface where lists ARE the pages;
it refuses the card-grid dashboard.
OWN-WORLD: pure-neutral near-black, hairline-separated full-bleed tables and
key-value stripes, signal-green primaries with near-black text, white-active
segmented filters, uppercase tracked table headers, ghost-row empty states.
STORY: the operator lands, reads one status line, scans a table, acts from the
toolbar or a row kebab.
FIRST VIEWPORT (any list page): breadcrumb → count-in-title → one gray
description line → toolbar (search · filters · refresh · primary right) →
the table, edge to edge.
FORM: NetBird console canon, played straight at reference craft level.

## Tokens (authoritative; exact values live in `src/index.css`)

### Color — chassis (pure neutral, no blue cast)
| Token | Value | Role |
|---|---|---|
| `--bg` | `#0A0A0B` | the one canvas (header, sidebar, and content share it) |
| `--bg-1` | `#101013` | barely-raised fills: table header band, kv stripe, hover |
| `--bg-2` | `#16161A` | inputs, secondary-button fill, selected rows |
| `--bg-3` | `#1D1D22` | menus, popovers, highest raised fills |
| `--border` | `rgba(255,255,255,0.07)` | hairline |
| `--border-hi` | `rgba(255,255,255,0.14)` | control borders, icon-square buttons |

### Color — ink
| Token | Value | Role |
|---|---|---|
| `--text` / `--fg` | `#EDEEF0` | primary ink |
| `--dim` | `#9EA1A8` | secondary ink (descriptions, secondary cell lines) |
| `--faint` | `#7E8188` | tertiary ink — clears 4.5:1 only up to `--bg-2`; a global CSS rule promotes `.faint` to `--dim` inside `--bg-3` popovers (menus, bell, rowmenu, chat history) |

### Color — accent (signal green) and status
| Token | Value | Role |
|---|---|---|
| `--accent` | `#3DF582` | links, active tab ink+underline, focus ring, brand marks |
| `--accent-hi` | `#6FF8A4` | hover of accent text |
| `--accent-ink` | `#07150C` | text ON green fills (≥12:1) |
| `--accent-dim` | `rgba(61,245,130,0.10)` | selected/active tinted fills (sparing) |
| `--ok` | `#4ACE7F` | healthy status dot/pill (green family is shared with the accent on purpose — status always carries a word, accent never sits on a status surface) |
| `--warn` | `#E5A83B` | building/pending/warning |
| `--crit` | `#F3655F` | failed/offline/danger |
| `*-dim` | 10–12% alpha | tinted banner/pill fills |

Nav active state is NEUTRAL (raised `--bg-2` pill, white ink) — the accent
never colors navigation, exactly as the reference reserves its orange.

### Type
- `--ui`: Inter Variable (self-hosted, air-gap requirement) — the only family.
- `--mono`: IBM Plex Mono — IPs, ids, versions, terminal, kbd. Never a costume.
- Scale: page title 26px/650/-0.015em (count baked in: "4 Apps") · entity name
  24px/650 · section heading 14px/600 · body 13.5px/400 · secondary 12.5px
  `--dim` · **table header 11px/600 UPPERCASE +0.06em `--faint`** (the one
  place tracked uppercase exists) · micro-mono 11px. Breadcrumb 12.5px.

### Space, shape, depth
- Spacing scale `--s-1..--s-7` (4/8/12/16/20/24/32) survives. Row height in
  tables is generous: two-line cells, ~64px.
- Radius: `--r-sm` 6 (chips, kbd), `--r-md` 8 (buttons, inputs, icon-squares,
  toggle blocks), `--r-lg` 10 (menus, modals, toasts ONLY). **Tables, kv
  stripes, sections, in-flow banners, and the page itself are square (0).**
- Depth: flat everywhere; real offset+blur shadow only on floating layers
  (menu, modal, palette, toast, drawer). No glows, no gradients.
- Motion: 120–180ms ease-out, state-conveying only. Reduced-motion kills all.

## Chrome

- **Top header (56px, full-width)**: brand wordmark + LITE tag left, then
  sidebar-collapse toggle; right side: help link (Support), notification bell,
  avatar circle (initials) opening the account menu (Profile, Log out).
  Hairline bottom border. No search box, no primary button in the chrome.
- **Update strip**: when an update is available, a full-width signal-green
  band ABOVE the header: "NEW" pill, one line, "See what's new →" to
  /changelog, dismiss X (session-dismissed). This replaces the sidebar
  update-chip. The offline banner remains a full-width crit strip under the
  header and outranks everything.
- **Sidebar (232px)**: org switcher at top; `Operate` group (Home, Apps,
  Machines, Network, Copilot), gap, `Workspace` group (Settings, Support,
  What's new). Active = raised neutral pill. Attention count chip stays on
  Home only. Bottom: **fleet card** — the one bordered card in the chrome:
  machines online n/n with dot, thin usage meter, version line; hosts the
  "Update available" CTA when the strip was dismissed.
- **Page-owned primaries**: "Deploy app" lives in the Apps toolbar and as the
  Home hero action — never in global chrome. "Add machine" lives in Machines'
  toolbar. ⌘K/Ctrl-K opens the palette (its own input); page-toolbar search
  inputs are plain local filters with no kbd chip.

## Page anatomy

- **List page** (Apps, Machines): breadcrumb (icon + section) → count-in-title
  → one `--dim` description line → **toolbar**: search input (magnifier,
  placeholder, kbd chip) · `Seg` status filter (white-active) · bordered
  refresh icon-square · primary right. → **the table, full-bleed**.
- **Detail page** (AppDetail, MachineDetail): breadcrumb ("Apps › name") →
  **entity header**: status dot + 24px name, one `--dim` meta line under;
  top-right `Cancel` + `Save changes` (disabled until dirty — replaces the
  bottom save bar everywhere). → **icon tabs**: icon+label, active = accent
  ink + 2px accent underline, hairline under the whole row. → content.
- **Overview tabs / info surfaces**: two columns — left a **kv stripe stack**
  (`.kv-rows`: full-width rows, icon + label left, value right in mono where
  it's data, per-row copy button, alternating `--bg-1` stripes, square,
  hairline-separated); right **setting blocks** (`.setting`): bold 14px label
  + `--dim` description + control (switch right for toggles), separated by
  whitespace and headings, NEVER boxed unless it is a toggle block
  (`--bg-1` fill, radius 8).
- **Forms pages** (Settings, Profile, Support compose): section heading +
  description + controls, stacked with generous whitespace; page-level dirty
  state saves from the page header, section-instant actions keep explicit
  Apply buttons. Focused flows (Deploy wizard, Auth) stay centered ≤760px.
- **Empty states** (`.empty-ghost`): decorative ghosted skeleton rows behind a
  centered icon-square, bold title, two `--dim` lines. The action does NOT
  move into the empty state — it stays in the toolbar.

## Component grammar

- **Table** (`table.table`): full-bleed; header band `--bg-1` with 11px
  uppercase tracked `--faint` labels + sort glyphs (`aria-sort`); tall
  two-line rows — primary 13.5px `--text` + secondary 12.5px `--dim`;
  status dot before name; mono for addresses/versions; hairline separators,
  square corners; hover `--bg-1`; row kebab = `.icon-sq` at row end opening
  the row menu; name cell holds the real `<Link>`.
- **kv stripes** (`.kv-rows` / `.kv-row`): label (icon + 13px `--dim`) left,
  value right-aligned (`--text`, mono when data), optional copy icon button.
- **Setting block** (`.setting`, `.setting.toggle`): toggle variant is a
  `--bg-1` radius-8 block with a `.switch` on the right; plain variant is
  unboxed heading + description + control.
- **Switch** (`.switch`): 36×20 track, `--bg-3` off / `--accent` on, white
  knob, keyboard focusable, `role="switch"`.
- **Buttons**: `.btn` secondary (dark `--bg-2` fill + `--border-hi`, white
  ink); `.btn.primary` solid `--accent` with `--accent-ink` text; `.btn.danger`
  crit outline; `.btn.ghost`; `.icon-sq` bordered 32px icon-square (refresh,
  kebab, copy — allowed icon-only with `aria-label`+`title`). All states:
  hover, focus ring (2px `--accent`, offset 2), disabled 45%, busy keeps label.
- **Segmented** (`.seg`): joined; **active segment = white fill, near-black
  text** (the reference's signature inversion). Status filters + view switches.
- **Tabs** (`.tabs`): icon+label, accent underline. Navigation only.
- **Status pills / dots**: dot (7px flat) + word; pills tinted 10% fills.
  Color never travels without the word. `ledClass()` stays the single map.
- **Inputs**: 38px, `--bg-2` fill, hairline, radius 8, focus = accent border;
  `Field` owns label association; kbd chips inside search inputs.
- **Meters** (`UsageBar`): 4px track, ok/warn/crit fill by threshold. The only
  quantitative graphic in the product now — charts, sparklines, and deltas are
  REMOVED (`charts.tsx` deleted; `/api/metrics/history` stays server-side,
  unused by the UI).
- **Banners**: in-flow notices are square, tinted 10% fill + 1px full tinted
  border, icon + one line + actions right. Chrome strips (update, offline) are
  full-width and square.
- **Overlays**: shared `useDialogBehavior` mandatory. Modal 560 radius-10;
  drawer 420 right (square edge); palette 560 top-centered; toasts BOTTOM-right,
  neutral raised, status icon in status ink, errors persist.
- **Empty/skeleton/terminal**: skeletons on every async list until `loaded`;
  terminal `#060607`, mono 12px/1.7; ghost-row empties per above.
- **Copilot chat**: full-page at `/copilot` (history rail of past chats left —
  hairline-separated rows, not boxes — thread right in a square hairline
  frame) and the 420px drawer; both share bubbles: user = `--accent-dim`
  fill, bot = `--bg-1`, radius 8 max.

## Interaction grammar

- Account menu = header avatar (Profile, Log out). Two-step logout preserved.
- Toolbar order on list pages: search → filters/segments → refresh → primary.
- Row actions: kebab menu (Start/Stop/Restart/Logs/Delete per state); primary
  navigation = row click + real link on the name.
- Save: ENTITY drafts (AppDetail) save from the entity header —
  `Cancel` + `Save changes` (+ `Save & redeploy` where the backend
  distinguishes), disabled until dirty. Section-scoped settings forms
  (Settings tabs) keep one explicit Save per section, beside the controls it
  saves. Closing/reloading the tab with a dirty draft warns
  (`useUnsavedChanges`); in-app route blocking needs the data router and is
  deferred. Instant-action surfaces keep explicit Apply. `.dirty-banner` is
  retired.
- Home order (minimal): title + one-line fleet status → attention rows (plain
  hairline rows, worst first) → setup checklist (only while onboarding) →
  machines summary rows → recent activity list. No charts, no KPI grid.
- Nav badges mean attention only. Bell badge counts open alerts only.
- Copy bans survive: zero em-dashes in visible strings; max one middot per
  metadata line. `<option>` labels carry names plus at most one short
  parenthetical of load-bearing state ("(offline)", "(score 87)") — decorative
  descriptions still live in Field hints.
- Filtered tables report what they hid: "Showing X of N" under the table
  whenever search/segments shrink the row set (and a clear-filters row when
  they shrink it to zero).

## Hard rules

1. Never render unreachable/loading as empty — gate on `loaded`, show the
   offline strip when `!reachable`.
2. No glows, gradients, colored side-stripes, nested boxes, or decorative
   shadows. A box must earn its border: floating layers, toggle blocks, and
   the fleet card only.
3. Inline styles only for genuinely dynamic values; reusable rules live in
   `index.css`.
4. Status color never decorates; identity facts are neutral; accent never
   colors status and never colors nav.
5. AA contrast on all ink tokens on all surfaces; focus ring on everything
   interactive; keyboard path for every flow; `prefers-reduced-motion` global.
6. Icon-only buttons are allowed as `.icon-sq`, the bell, and the standing
   micro-affordances (toast/strip dismiss X, password eye, chat-row delete,
   composer Send) — every one with `aria-label` (+`title` where hoverable).
   Anything else in chrome or sections carries a visible label.
7. The `rackops-org-changed` + localStorage org contract and all API behavior
   stay intact. Air-gap: no CDN assets ever.

## Migration map (Machined console → Quiet console)

| Old | New |
|---|---|
| `.card` stacks as page structure | full-bleed sections, `.kv-rows`, `.setting` blocks |
| azure `--accent #57A2FF` | signal green `#3DF582` (+ `--accent-ink` on fills) |
| blue-graphite chassis (`#0B0E11`…) | pure-neutral (`#0A0A0B`…) |
| topbar search + global Deploy + sidebar account | header (bell/help/avatar) + page-owned primaries + toolbar search |
| `.dirty-banner` bottom save bar | header Cancel/Save (disabled-until-dirty) |
| sidebar `.update-chip` | full-width update strip above header + fleet card CTA |
| `TrendChart`/`Delta`/`Sparkline`/KPI hero | removed (UsageBar only) |
| radius-10 bordered tables/cards | square full-bleed tables/stripes |
| accent-tinted active nav pill | neutral raised nav pill |
| sentence-case table headers | 11px uppercase tracked headers (tables only) |
