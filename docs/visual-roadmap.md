# Visual changes worth building

This document records implemented and proposed visual work. Open [the interactive concept](assets/workspace-concept.html) in a browser to compare the original directions using illustrative data. The real application remains a terminal UI; the browser typography and effects are a design reference.

## 1. Compact workspace — implemented

**Problem:** the 120×30 dashboard spends much of its height on the header, telemetry, selection card and repeated shortcuts. Reading a unit configuration or a long Docker mount requires frequent scrolling.

**Delivered:** one identity row, a compact telemetry strip, an explicit five-suite rail, visible Control Deck and saved-view entry points, then the inventory and inspector. The selected identity is compressed to one content row. Focus retains a bright edge while secondary borders recede. `[` / `]` adjust inventory/inspector proportions from 30–70%, and saved views restore that split.

**Value:** several more rows of useful content, a clearer selected workload, and a deliberate visual hierarchy. Saved views become visible entry points instead of living only in Actions.

**Implementation:** `layoutDashboard`, selection card, chrome/footer, preferences and the saved-view schema share the compact model. Existing navigation, responsive stacking and zoom are preserved. Mouse dragging remains a possible follow-up.

**Verification:** the actual 120×30 cell fixture gains six content rows over the prior layout. Tiny-screen tests retain essential content, saved-view tests cover pane ratio, and all themes retain border plus selection-pointer focus cues.

## 2. Timer timeline — distinctive and useful

**Problem:** timestamps answer when a job runs, but are harder to compare at a glance.

**Proposal:** a six-hour, terminal-cell timeline above the existing timer list, with markers for next runs and a separate unscheduled group. Selection links timeline and table. Show exact next/last timestamps beneath it, plus the triggered unit and links to status/logs. Use text labels for due and unscheduled states.

**Value:** maintenance jobs, backups and collisions become easier to spot. It adds visual interest using actual scheduling data.

**Scope:** extend timers.go with a pure timeline projection and renderer. Refresh without losing selection. A next trigger in the past is “due / awaiting update”, not automatically a failed or missed job. Last trigger time is not proof of success. Do not invent recurring runs from a single next timestamp.

**Acceptance:** null/infinite timestamps stay unscheduled; multiple markers in one cell retain a count and selectable list; timezone and time-window labels remain visible; narrow terminals retain the table without a cramped timeline.

## 3. Troubleshooting workspace — highest workflow value

**Problem:** status, logs, observed activity and exports are separate destinations. Moving between them loses the visual thread of an investigation.

**Proposal:** a compact status band, a large log-search panel with a match counter and numbered context, and a smaller evidence panel with collection times and snapshot sections. Keep the target and host fixed in the header. Let users open the existing editable snapshot review from the evidence panel.

**Value:** users can read the failure and its surrounding evidence together. The strongest visual treatment goes to the selected match and actual error states.

**Scope:** compose existing log-search results, activity and snapshot controls into a dedicated layout. Add individual section collection states and timestamps. Avoid implying that separately sampled evidence is one atomic capture. No automatic diagnosis or background sharing.

**Acceptance:** switching the main selection cannot silently retarget an open investigation; retained logs are labelled separately from newly fetched snapshot logs; missing data and collection errors are explicit; editing and private, non-overwriting export remain available.

## Suggested sequence

1. Timer timeline: medium change, builds on the read-only timer browser.
2. Troubleshooting workspace: larger change, combines the search and report tools.

Verify actual tcell frames at 160×44, 120×30 and 80×24 across all themes, including long names, empty results, unavailable backends and overlays. Browser mockups cannot establish terminal readability, geometry or keyboard correctness.
