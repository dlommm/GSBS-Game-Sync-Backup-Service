# Widget Grid Dashboard Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A fifth layout, `widgets` ("Custom"), where the five dashboard panels (stats, recent games, activity, devices, live pulse) render in a user-arranged order with optional hiding — arranged in an accessible edit mode (move/hide buttons + drag-drop enhancement) and stored per user.

**Architecture:** Reuses migration-33 `user_prefs` under key `dashboard.widgets` (JSON `{"order":[ids],"hidden":[ids]}`, validated against the fixed id set). The dashboard template's five panels are extracted into named `widget_<id>` defines; the default layouts keep today's static arrangement by calling those defines in place, while `layout=widgets` ranges over the server-computed ordered list into a spanning CSS grid — server-rendered order, zero flash, zero CSP exceptions. Edit mode is plain buttons (keyboard accessible) that reorder/hide DOM nodes and mirror state into hidden form fields; HTML5 drag-drop is a pointer enhancement over the same state. Save is a normal CSRF'd POST + PRG.

**Tech Stack:** Go (webui handler + validation), html/template defines, vanilla JS in app.js, CSS layer in input.css. No new tables, no new deps.

## Global Constraints

- Widget ids (fixed): `stats, games, activity, devices, pulse`. Unknown ids are dropped on read AND rejected on write; missing ids append in default order (forward compatibility when new widgets ship).
- `widgets` joins `validLayout` server-side, the client validator, theme-boot's layout list, and `layoutChoices` (label "Custom").
- Strict CSP (no inline script/style); template_names_test registration for every new define; ui-smoke + full webui tests green before each commit.

### Task 1: widget config codec + validation (webui)
Files: `server/webui/widgets.go` (create), `server/webui/widgets_test.go` (create).
Produces: `type widgetConfig struct { Order []string; Hidden []string }`; `parseWidgetConfig(raw string) widgetConfig` (tolerant: bad JSON → default); `(widgetConfig) visibleOrder() []string`; `widgetConfigFromForm(order, hidden string) (widgetConfig, bool)` (csv inputs, strict); `(widgetConfig) marshal() string`; `defaultWidgetOrder = []string{"stats","games","activity","devices","pulse"}`.
Tests: round-trip, unknown-id drop, missing-id append, strict form rejection, empty raw → default.

### Task 2: dashboard template widgets
Files: `server/webui/templates/dashboard.html` (extract `widget_stats|games|activity|devices|pulse` defines; static path calls them; `{{if eq .Layout "widgets"}}` path ranges `.Widgets` via a `dashWidget` funcMap helper executing "widget_"+id), `server/webui/render.go` (helper + `dashboardData.Widgets []string`), `server/webui/template_names_test.go` (register defines; dashboardData test literal gains Widgets).

### Task 3: handler + save endpoint
Files: `server/webui/handlers_dashboard.go` (populate Widgets when layout==widgets + `handleWidgetsSave`), `server/webui/router.go` (`POST /dashboard/widgets/save`), `server/webui/handlers_auth.go` (flash `widgets_saved`), `server/webui/appearance_test.go` (extend: save round-trip renders new order; reset clears pref).

### Task 4: edit mode UI
Files: `server/webui/templates/dashboard.html` (Customize bar: Customize/Save/Reset buttons + hidden form, per-widget control cluster rendered only in widgets layout), `server/webui/static/app.js` (`initWidgetEditor`: toggle body class, ↑/↓/hide handlers reorder DOM + rebuild csv state, HTML5 DnD in edit mode), `server/webui/static/src/input.css` (`.dash-widgets` grid: 2-col, stats+games span 2; `.widget-controls` chrome; `body.widgets-editing` affordances).

### Task 5: picker + client + docs
Files: `server/webui/render.go` (`validLayout` + `layoutChoices` add widgets/"Custom"), `client/webui/render.go` (`clientValidLayout` — client renders widgets layout as default sidebar since the local dashboard has different panels; accept the value, no-op there), `server/webui/static/theme-boot.js` (+`'widgets'`), CHANGELOG `[Unreleased]`.
Note: on the client's local pages `widgets` behaves as the default layout (its dashboard is a different page); the value is accepted so the synced pref never bounces.

### Out of scope (later)
Free-form resize, per-widget settings, admin-portal widgets.
