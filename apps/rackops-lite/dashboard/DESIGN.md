# RackOps Lite dashboard — design system

**World: "Machined console."** A refined dark control plane in the
Linear/Vercel/Cloudflare craft register. Cool graphite neutrals, one calm azure
accent, hairline borders, flat surfaces, mono for data. No hardware
skeuomorphism: the previous world's phosphor-amber glows, LED blink animations,
rack mounting-hole rails, and gradient slats are removed, not softened.

Provenance: full visual-world replacement, user-directed 2026-07-26
("refined dark console", accent delegated). Mode: **Operate** on every routed
page. Physical scene: an operator checking their rack from a laptop in the
evening or a dim office — dark, calm, zero spectacle.

## Tokens (authoritative; exact values live in `src/index.css`)

### Color — chassis
| Token | Value | Role |
|---|---|---|
| `--bg` | `#0B0E11` | app canvas |
| `--bg-1` | `#12161A` | cards, sidebar-adjacent surfaces |
| `--bg-2` | `#181D22` | inputs, hovered rows, inset panels |
| `--bg-3` | `#1F252B` | menus, highest raised fills |
| `--border` | `rgba(255,255,255,0.08)` | hairline |
| `--border-hi` | `rgba(255,255,255,0.15)` | strong hairline (controls, overlays) |

### Color — ink
| Token | Value | Role |
|---|---|---|
| `--text` / `--fg` | `#E7EAEE` | primary ink |
| `--dim` | `#9CA6B0` | secondary ink |
| `--faint` | `#8C96A2` | tertiary ink — still ≥4.5:1 on every surface up to `--bg-3` |

### Color — accent (azure) and status
| Token | Value | Role |
|---|---|---|
| `--accent` | `#57A2FF` | links, active nav, selection text, focus ring |
| `--accent-hi` | `#82BAFF` | hover state of accent text |
| `--accent-strong` | `#2969CC` | primary button fill (white text, 5.3:1) |
| `--accent-strong-hi` | `#2C6FD1` | primary button hover fill (white text, 4.9:1) |
| `--accent-dim` | `rgba(87,162,255,0.12)` | selected/active backgrounds |
| `--ok` | `#4ACE7F` | healthy (dot, badge, meter) |
| `--warn` | `#E5A83B` | building/pending/warning |
| `--crit` | `#F3655F` | failed/offline/danger |
| `*-dim` | 12% alpha of each | tinted badge/banner fills |

Status is semantic only: never used decoratively, never for hardware-vendor
tags, always paired with a word or icon. The old purple/GPU and cyan colors are
gone — hardware facts wear neutral `.tag`s.

### Type
- `--ui`: Inter Variable (self-hosted) — the only UI family. Space Grotesk is
  removed from the product.
- `--mono`: IBM Plex Mono — data, ids, metrics, terminal, kbd. Never as a
  "technical costume" on prose.
- Scale (rem-fixed): page title 20px/650/-0.01em · section 14px/600 ·
  body 13.5px/400 · secondary 12.5px · label 12px/500 (sentence case — the old
  tracked-uppercase eyebrow grammar is retired) · micro-mono 11px (U-numbers,
  kbd, timestamps).

### Shape, depth, motion
- Radius: 6 / 8 / 10 (`--r-sm/md/lg`). Cards 10, controls 8, chips/badges full.
- Depth: cards are FLAT (border only, no shadow, no inset edge-light).
  Only floating layers (menus, modals, palette, toasts) get a real shadow:
  `0 8px 24px rgba(0,0,0,.45)` class of offset+blur — never colored halos.
- Motion: 120–180ms ease-out, state-conveying only (hover/focus/open). Page
  transition is a 150ms opacity fade, no translate. No blink/pulse keyframes
  anywhere; reduced-motion kills the rest.

## Component grammar

- **Status dot** (`.led` classes kept for API compatibility): 7px flat circle,
  no glow, no animation. Always adjacent to a word (`StatusBadge`) or an
  sr-only label. `ledClass()` in `ui.tsx` stays the single status→state map.
- **Badges** (`.badge.ok|warn|crit|accent|neutral`): 11px, tinted fill + status
  ink. Hue-named classes (`green/red/yellow/amber/gray/purple/cyan`) are gone.
- **Buttons**: `.btn` neutral (transparent→`--bg-2` hover, `--border-hi`
  border); `.btn.primary` solid `--accent-strong`, white text; `.btn.danger`
  red text/border on hover; `.btn.ghost`; `.sm`. All states: hover, focus ring,
  disabled 45%, busy (Spinner inside, label stays).
- **Inputs**: `--bg-2` fill, hairline border, focus = accent border. One
  `Field` component owns label association; error text under the control.
- **Nav**: active item = `--accent-dim` fill + `--accent` ink (filled pill, no
  left bar). Sidebar sections are 11px sentence-case dim labels.
- **KPI/stat tile**: label 12px `--dim` sentence case, value 22px mono
  `--text`, unit 12px `--dim`. Value ink never wears a data color (dataviz
  rule: text wears text tokens).
- **Meters** (`UsageBar`): 4px track `rgba(255,255,255,0.07)`, fill
  ok/warn/crit by threshold, scaleX animation only. Always beside a numeric
  readout. Percentages only — never feed it a temperature.
- **Sparkline**: single-series accent bars at 70% opacity, no axis chrome,
  `aria-hidden` with the numeric value visible beside it.
- **The rack** (signature, de-skeuomorphed): a flat bordered panel; each
  machine is a row with a mono slot number in a hairline-separated left rail
  column, status dot + word, name, and meters. No mounting holes, no gradients.
- **Lists**: `.list` panel of hairline-separated `.list-row`s replaces ad-hoc
  inline `borderTop` rows (members, tokens, deployments, tickets).
- **Key–value**: `.kv` grid (dt `--dim` 12.5px / dd mono 12.5px) replaces raw
  `<table>`s in AppDetail and Profile.
- **Overlays**: shared `useDialogBehavior` (Esc/trap/restore) is mandatory.
  Modal 560 default; drawer 420 right; palette 560 top-centered. Overlay
  scrim `rgba(5,8,12,0.65)`.
- **Toasts**: neutral raised surface, status icon in status ink, NO colored
  side-stripe. Errors persist until dismissed (`role="alert"`); info/ok
  auto-dismiss with pause-on-hover.
- **Banners** (offline, update, dirty-config): full tinted border + `*-dim`
  fill, icon, one-line message, actions right (`.offline-banner`,
  `.update-banner`, `.dirty-banner`). Offline outranks everything (crit);
  update is accent. Status-edged cards use `.card.ok/.warn/.crit/.accent` —
  never hand-rolled rgba border literals.
- **Terminal/logs**: `#0A0C0F`, mono 12px/1.7, timestamps `--faint`, kind tag
  `--accent`. Max-heights per context.
- **Empty states**: solid hairline border (not dashed), icon in a soft
  `--bg-2` circle, title + one sentence + one primary action.
- **Skeletons**: `--bg-2` blocks with 1.4s sweep; every async list shows
  skeletons until `loaded`, never a false empty.

## Hard rules

1. Never render unreachable/loading as empty — gate empties on `loaded`,
   show the offline banner when `!reachable`.
2. No glows, blinks, gradient surfaces, colored left-stripes, or nested cards
   beyond card > inset.
3. Inline styles only for genuinely dynamic values (computed widths, heights);
   everything reusable lives in `index.css` classes.
4. Status color is never decoration; identity facts are neutral.
5. AA contrast on all ink tokens against all four surfaces; focus ring
   (2px `--accent`, 2px offset) on every interactive element.
6. Icon-only buttons are banned in primary chrome — visible labels, or
   `aria-label` + `title` only for tight table-row actions.
7. Keep the `rackops-org-changed` + localStorage org contract and all API
   behavior intact — this is a reskin plus UX repair, not a rewrite.

## Migration map (old world → new)

| Old | New |
|---|---|
| `var(--amber)` as brand accent | `var(--accent)` |
| `var(--amber)` as warning/pending | `var(--warn)` |
| `--display` font refs | remove (single family) |
| `.badge.green/.red/.yellow` | `.badge.ok/.crit/.warn` |
| `.badge.amber` | `.badge.accent` (or `.warn` if it meant warning) |
| `.badge.gray/.purple/.cyan` | `.badge.neutral` or `.tag` |
| `.hero-banner` | `.status-banner` |
| LED glow/blink semantics | flat dot; urgency via banners/badges |
| tracked-uppercase `.eyebrow` | same class, restyled sentence-case label |
| raw `<table>` key-values | `.kv` |
| inline `borderTop` row lists | `.list` / `.list-row` |
