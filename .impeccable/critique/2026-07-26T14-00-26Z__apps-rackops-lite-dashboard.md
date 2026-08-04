---
target: apps/rackops-lite/dashboard
total_score: 30
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 4
timestamp: 2026-07-26T14-00-26Z
slug: apps-rackops-lite-dashboard
---
Method: dual-agent (A: design-review sub-agent · B: detector-scan sub-agent). No browser automation was exposed in this session, so both assessments worked from source code; the browser overlay flow was skipped (fallback signal: CLI detector scan only).

## Design Health Score

| # | Heuristic | Score | Key Issue |
|---|-----------|-------|-----------|
| 1 | Visibility of System Status | 3 | No first-load state; `App.tsx:56` renders a blank screen while auth resolves, and `DataProvider` (`data.tsx:29-36`) has no loaded flag, so a real fleet briefly renders as a fresh install |
| 2 | Match System / Real World | 4 | Solid — domain-native copy throughout ("Machines", registrar-style DNS card, "Maps 8081 → 8080" port echo) |
| 3 | User Control and Freedom | 3 | Escape closes only the command palette; `Modal` (`ui.tsx:142-157`), confirm dialog, and Copilot drawer have no Esc handling; no undo anywhere |
| 4 | Consistency and Standards | 3 | Three implementations of "clickable row" (`div onClick` in `Apps.tsx:68`, `Link` in `Machines.tsx:239`, `button` in `Rack.tsx:15`) |
| 5 | Error Prevention | 3 | Excellent consequence-stating confirms, but no type-to-confirm on app deletion; deploy name validated only as non-empty |
| 6 | Recognition Rather Than Recall | 3 | Sidebar footer is four icon-only buttons distinguished only by `title` (`App.tsx:143-156`); nav collapses to unlabeled icons under 900px |
| 7 | Flexibility and Efficiency | 2 | ⌘K palette only navigates — no verbs, no bulk operations, no other shortcuts; filter box hidden until >5 apps |
| 8 | Aesthetic and Minimalist Design | 3 | Disciplined ink hierarchy and one-accent system; Home stacks six competing sections |
| 9 | Error Recovery | 3 | Great recovery copy where errors are anticipated, but every data fetch swallows failure silently (`data.tsx:39-44`) |
| 10 | Help and Documentation | 3 | InfoTips, wizard, LED legend are strong contextual help; no searchable reference docs beyond Swagger |
| **Total** | | **30/40** | **Good** |

## Design Specificity Verdict

**Authored for RackOps — this could not ship unchanged under another product's name.**

**LLM assessment:** The design system is themed, not generic: the tokens file opens with an authored thesis ("an operator's console… one surgical phosphor-amber accent, LED status language, rack-unit structure") and delivers on it. A signature element recurs — the rack visualization with U-labels, mounting-hole rails, and per-slat meters; even the brand mark is a miniature rack unit with a live fleet LED, and the auth backdrop is fading rack slats. One function (`ledClass`, `ui.tsx:51-62`) is the single source of truth for status → LED across every surface, with hardware-accurate blink/pulse semantics. Copy is domain-native and non-transplantable. Slips: Settings is generic SaaS form-stack; changelog dates wear amber status badges; `.badge.cyan` is a dead alias of amber. Biggest missed opportunity: the rack metaphor stops at navigation — it's a picture you click through, not a surface you operate on. Relative to the EnvKit/Cloudflare taste references, the hairline-border, mono-for-data discipline is genuinely in that family, pushed into a darker hardware register (dark-only, deliberately).

**Deterministic scan:** 5 findings, all in `src/index.css`, zero across 25 TS/TSX files. Three `side-tab` hits (`index.css:524-526`) are judged **false positives** — they're one status-stripe pattern on the transient toast component, a semantic convention, not decorative card accents. Two true positives: `overused-font` (`index.css:1`, Space Grotesk + Inter via Google Fonts — accurate detection; keeping them is a legitimate taste call since the tokens deliberately encode the EnvKit/Cloudflare register, but the CDN import itself clashes with the self-hosted/offline pitch) and `layout-transition` (`index.css:342`, `.bar-fill` animates `width` — a real layout-thrash flag the design review missed; minor impact on 4–7px meter bars, `transform: scaleX()` would fix it).

**Visual overlays:** Not available — no browser automation tool is exposed in this session, so no user-visible overlay exists; the CLI scan above is the fallback signal.

## Overall Impression

This is a genuinely authored operator's console with a real design system — one status language, consequence-literate copy, and a deploy flow deliberately engineered to end on "your app is live" rather than a closed dialog. Cognitive load is low (progressive disclosure is demonstrably understood; only the 8-chip template category row exceeds the ≤4 guideline). The single biggest opportunity is the failure story: the moment the operator needs the dashboard most — control plane down — is currently styled identically to a brand-new install, and the accessibility floor (keyboard paths, label association, dialog behavior) hasn't caught up with the visual craft.

## What's Working

1. **A real, systemic status language.** `ledClass()` maps every backend status to one of four LED states used across nav, rack, apps, deployments, domains, and tickets, with crit-blinks/warn-pulses suppressed under `prefers-reduced-motion` (`index.css:73-75`, `222-223`). This is how design systems are supposed to be built.
2. **Consequence-literate copy at every dangerous moment.** Confirms state blast radius and what survives ("Data in managed volumes stays on disk…", `AppDetail.tsx:124`); the offline-machine card hands you the exact `systemctl` command (`Machines.tsx:104`); the public-access wizard turns CGNAT into three priced, honest options (`Network.tsx:426-450`).
3. **Truthful async state.** Deploy buttons lock for the whole deploy window based on live SSE status, not just the local HTTP call (`Apps.tsx:62-66`), and the alert poll converges the UI to server truth so stale attention cards can't linger (`data.tsx:94-100`).

## Priority Issues

1. **[P1] Server unreachable is indistinguishable from a fresh install.** Every fetch in `data.tsx:39-44` swallows errors; an operator's worst moment (control plane down) renders as cheerful onboarding ("Your rack is waiting for its first machine"). **Fix:** track last-successful-fetch + SSE health in `DataProvider`; show a persistent "Can't reach the server — retrying" banner and suppress empty/onboarding states until one successful load. **Suggested command:** /impeccable harden
2. **[P1] Primary list rows are keyboard-inaccessible.** App cards on Apps and Home are `<div onClick>` with no tabIndex/role/key handler (`Apps.tsx:68`, `Home.tsx:105`), same for ticket and deployment rows. A keyboard-only user cannot open an app from the Apps page — the core flow. The fix pattern already exists in-repo (`Rack.tsx` uses `<button>`, MachineDetail uses `<Link>`). **Fix:** make row cards links or buttons. **Suggested command:** /impeccable audit
3. **[P1] Form labels are not programmatically associated.** The universal `.field` pattern has `<label>` as a sibling with no `htmlFor`/`id` (`Auth.tsx:127-129`, `DeployFlow.tsx:262-265`, all of `SettingsPage.tsx`) — screen readers announce every input in the product as unlabeled. **Fix:** one shared `Field` component with id/htmlFor. **Suggested command:** /impeccable audit
4. **[P1] Dialogs lack Escape, focus trap, and focus return.** `Modal` (`ui.tsx:142-157`), confirm (`confirm.tsx:41-56`), and the Copilot drawer close only via click; focus tabs behind the overlay. **Fix:** shared dialog behavior (Esc, trap, restore, `aria-labelledby`) in `Modal`, reused everywhere. **Suggested command:** /impeccable audit
5. **[P2] No first-load state and a blank auth gate.** `App.tsx:56` returns `null` while auth resolves; no `loaded` flag means every hard load flashes onboarding/empty UI. **Fix:** a `loaded` boolean gating empty states plus a branded splash (the rack-unit mark with a pulsing LED would be on-theme). **Suggested command:** /impeccable polish
6. **[P2] The power path stops at navigation.** ⌘K can't act on entities, no bulk operations, no other shortcuts — for an Operate surface this caps expert throughput (Heuristic 7 = 2). **Fix:** palette verbs ("restart *app*", "logs *app*") and multi-select on Apps. **Suggested command:** /impeccable shape

## Persona Red Flags

**Alex (Power User):** ⌘K yields navigation only — restarting a failing app means navigate → find row → icon button; three failing apps = three separate restart clicks with no "restart all failing". Esc does nothing on modals/confirms. The filter box only appears above 5 apps (`Apps.tsx:48`). No log download/copy-all in `LogsTab`. On Linux — this audience *is* Linux — the search trigger permanently says "⌘K" (`App.tsx:164`) instead of Ctrl+K. Wins: Enter-to-advance, no nag confirms on start/stop/restart, self-re-arming live tail.

**Sam (Accessibility-Dependent):** **Blocked** — cannot open an app from Apps/Home at all (clickable divs, no keyboard path). Hears every form input as unlabeled. Rack units never announce online/offline — the LED is aria-hidden with no text fallback (`Rack.tsx:17-23`), unlike `StatusBadge` which pairs LED with a word. Can Tab straight out of open modals into the page behind. `.faint` text (#77777f at 11.5px) computes to roughly 4.2:1 — just below WCAG AA — and carries load-bearing content. Wins: visible amber `:focus-visible` outline, `aria-live` toast region, `role="alertdialog"` on confirm.

**Riley (Stress Tester):** Refresh mid-deploy at step 2 loses all wizard state (component-local, `DeployFlow.tsx:33-57`) and a re-run can double-deploy; at step 3 a refresh abandons the launch view. Killing the API server shows onboarding, not an outage. Long app names have no ellipsis handling in `u-name` and Home rows, unlike the org switcher which handles it.

## Minor Observations

- Google Fonts loaded from CDN (`index.css:1`) in a product pitched as self-hosted/offline — air-gapped racks get fallback fonts and a hanging request; self-host the three families. (Detector's `overused-font` hit lands on the same line.)
- `.bar-fill` animates `width` (`index.css:342`, detector-caught); `transform: scaleX()` avoids layout work.
- `.badge.cyan` is a dead alias of amber; badge classes are hue-named (`green/red/yellow`) rather than semantic (`ok/crit/warn`), unlike the LED system.
- Auth and Invitation pages hand-roll error cleaning instead of the shared `cleanError` (`Auth.tsx:43,63`).
- Changelog dates styled as amber status badges (`Changelog.tsx:57`) — status vocabulary on metadata.
- Support thread back control is a literal "←" text glyph (`Support.tsx:204`) vs `ArrowLeft` icons elsewhere.
- Icon-only Stop/Restart buttons rely on `title` (`Apps.tsx:77-86`) — invisible on touch.
- `InfoTip` bubble is `role="tooltip"` but not linked via `aria-describedby` (`ui.tsx:20-21`).
- Update flow hard-reloads at 60s regardless of outcome (`App.tsx:239`) — a failed update reloads with no explanation.
- Env-var "secret" toggle exists in AppDetail settings but not in the DeployFlow env editor.
- Error toasts vanish after 7s with no way to re-read (`toast.tsx:34`).

## Questions to Consider

1. What should the dashboard look like at the moment the operator needs it most — when the control plane is down? If that moment were designed with the same care as the deploy flow's ending, what would a "degraded but honest" shell show?
2. The rack is your signature — why is it only a doorway? What happens if it becomes the operating surface: drop an app onto a unit to migrate it, see the edge proxy drawn on the rack, spot the hot machine by its glow?
3. If ⌘K is the promise of speed, why does it only move you around? The palette already knows every app and machine by name and status — the distance from "jump to jellyfin" to "restart jellyfin" is small in code and enormous in operator throughput.
