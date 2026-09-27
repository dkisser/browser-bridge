---
name: Liquid Bridge Glass
colors:
  surface: '#131315'
  surface-dim: '#131315'
  surface-bright: '#39393b'
  surface-container-lowest: '#0e0e10'
  surface-container-low: '#1b1b1d'
  surface-container: '#1f1f21'
  surface-container-high: '#2a2a2c'
  surface-container-highest: '#353437'
  on-surface: '#e4e2e4'
  on-surface-variant: '#c0c6d6'
  inverse-surface: '#e4e2e4'
  inverse-on-surface: '#303032'
  outline: '#8b91a0'
  outline-variant: '#414754'
  surface-tint: '#aac7ff'
  primary: '#aac7ff'
  on-primary: '#003064'
  primary-container: '#3e90ff'
  on-primary-container: '#002957'
  inverse-primary: '#005db8'
  secondary: '#47e266'
  on-secondary: '#003910'
  secondary-container: '#09bf49'
  on-secondary-container: '#004615'
  tertiary: '#ffb4aa'
  on-tertiary: '#690004'
  tertiary-container: '#ff5447'
  on-tertiary-container: '#5c0003'
  error: '#ffb4ab'
  on-error: '#690005'
  error-container: '#93000a'
  on-error-container: '#ffdad6'
  primary-fixed: '#d6e3ff'
  primary-fixed-dim: '#aac7ff'
  on-primary-fixed: '#001b3e'
  on-primary-fixed-variant: '#00468d'
  secondary-fixed: '#6cff82'
  secondary-fixed-dim: '#47e266'
  on-secondary-fixed: '#002106'
  on-secondary-fixed-variant: '#00531a'
  tertiary-fixed: '#ffdad5'
  tertiary-fixed-dim: '#ffb4aa'
  on-tertiary-fixed: '#410001'
  on-tertiary-fixed-variant: '#930007'
  background: '#131315'
  on-background: '#e4e2e4'
  surface-variant: '#353437'
typography:
  headline-lg:
    fontFamily: SF Pro Display
    fontSize: 18px
    fontWeight: '600'
    lineHeight: 24px
  headline-md:
    fontFamily: SF Pro Display
    fontSize: 15px
    fontWeight: '600'
    lineHeight: 20px
  body-md:
    fontFamily: SF Pro Text
    fontSize: 13px
    fontWeight: '400'
    lineHeight: 18px
  body-sm:
    fontFamily: SF Pro Text
    fontSize: 12px
    fontWeight: '400'
    lineHeight: 16px
  label-md:
    fontFamily: SF Pro Text
    fontSize: 11px
    fontWeight: '600'
    lineHeight: 14px
  label-sm:
    fontFamily: SF Pro Text
    fontSize: 10px
    fontWeight: '700'
    lineHeight: 12px
  code-sm:
    fontFamily: JetBrains Mono
    fontSize: 11px
    fontWeight: '500'
    lineHeight: 14px
rounded:
  sm: 0.25rem
  DEFAULT: 0.5rem
  md: 0.75rem
  lg: 1rem
  xl: 1.5rem
  full: 9999px
spacing:
  gutter: 0.75rem
  margin: 1rem
  space-xs: 0.25rem
  space-sm: 0.5rem
  space-md: 0.75rem
  space-lg: 1rem
  space-xl: 1.5rem
---

## Brand & Style

This design system elevates browser agent oversight from a crude utility pane into a precision-engineered, liquid glass cockpit inspired by macOS Sonoma and visionOS. Built for engineers, power users, and enterprise operators supervising autonomous AI browser sessions, it balances immediate supervisory authority with ethereal translucent beauty. 

The aesthetic is high-fidelity **Liquid Glassmorphism**:
- Translucent backdrop blurs that dynamically filter page content behind the panel.
- Specular boundary highlights (sub-pixel top and left inner borders that catch light).
- Physical tactile weight on safety-critical interactions—most notably the "Takeover" master safety kill-switch and instant permission approvals.
- High visual legibility through optical isolation, ensuring critical security prompts and origin states are never obscured or washed out by complex browser backgrounds.

## Colors

The system uses an adaptive dark-mode primary baseline tailored for high-contrast safety and specular luminance, with full responsive mirroring for light environments:

- **Primary (`#0A84FF`)**: Electric system azure. Represents connectivity, primary operational focus, active navigation segments, and selected session links.
- **Secondary (`#30D158`)**: Vivid signal emerald. Designated for verified session health, low-friction "Allow Always" authorizations, and paired bridge states.
- **Tertiary (`#FF453A`)**: Precognitive safety crimson. Anchors emergency agent rejection, security blocklists, and immediate takeover overrides.
- **Neutral (`#1C1C1E`)**: Deep volcanic substrate. Operates at variable translucent opacities (e.g., `rgba(28, 28, 30, 0.72)`) across backing tiers to synthesize frosted visual depth.

### Functional Status Tokens
- **Amber Warning (`#FF9F0A`)**: Signals "Allow Once" ephemeral execution and pending verification pauses.
- **Surface Rim / Highlight (`rgba(255, 255, 255, 0.16)`)**: Ultra-fine linear light bounce along top glass edges.

## Typography

Typography prioritizes micro-density readability within a narrow 340px–420px browser companion drawer. Built on Apple's San Francisco system typography (with fallback to Inter or Geist), technical identifiers and session addresses utilize `JetBrains Mono` for rapid origin audits:

- **Section Headers & Eyebrows (`label-sm`)**: Uppercase tracking (+0.06em) in subdued neutral opacity to introduce section demarcation without visual clutter.
- **Agent IDs & Origin URLs (`code-sm`)**: Monospaced tabular alignment ensuring UUIDs and TLS domain paths align cleanly across stacked approval queues.
- **Action Triggers & Status Labels (`label-md`, `body-sm`)**: Medium-weight humanist letterforms designed to render crisply over translucent, filtered backgrounds.

## Layout & Spacing

The layout is constructed for a persistent, collapsible sidebar panel (standard width: `360px`, expandable to `440px`).

- **Grid Hierarchy**: Single-column vertical stream governed by an 8pt architectural rhythm, contracting to 4pt micro-gaps for related action button groups.
- **Top Bar Fixed Anchor**: The status bar, session fingerprint, and Master Takeover switch remain pinned at the panel apex, retaining 100% immediate interaction priority regardless of queue scroll depth.
- **Segmented Filter Bar**: Resides immediately below the primary control cluster, dividing the scrollable canvas into dedicated panels (`Approvals`, `Origins`, `Blocklist`, `Activity`).
- **Dynamic Content Rail**: Vertically scrolls with a customized ultra-thin translucent track, incorporating bottom safe-area padding (`space-xl`) to prevent clipping above browser status bars.

## Elevation & Depth

Visual hierarchy does not rely on opaque shadows. It uses optic material tiers (visionOS depth layering):

1. **Backdrop Canvas (Base Layer)**: 
   - `background: rgba(20, 20, 22, 0.65)` with `backdrop-filter: blur(40px) saturate(190%)`.
   - Mimics thick structural smoked glass separating the webpage DOM from supervisory tools.
2. **Elevated Cards & Master Pods (Mid Layer)**: 
   - `background: rgba(255, 255, 255, 0.05)`.
   - Border: Sub-pixel 1px solid composite (`rgba(255, 255, 255, 0.12)` top edge, `rgba(255, 255, 255, 0.04)` bottom edge).
   - Drop Shadow: `0 8px 32px 0 rgba(0, 0, 0, 0.32)`.
3. **Interactive Chips & Tactical Controls (Top Floating Layer)**:
   - `background: rgba(255, 255, 255, 0.1)`.
   - Inset light reflection: `inset 0 1px 1px 0 rgba(255, 255, 255, 0.25)`.
   - Hover transition: shifts backdrop tint to `rgba(255, 255, 255, 0.18)` with an ambient outer glow matching the state color.

## Shapes

The shape vocabulary uses Apple-style continuous squircle geometry (smooth corner radii):

- **Master Floating Cards & Modals**: `16px` (`rounded-xl`) corner curvature creating cohesive containment for action groups.
- **Interactive Controls & Approval Buttons**: `10px` (`rounded-md`) matching modern macOS Control Center items.
- **Pills, Switches & Realtime Badges**: Fully rounded continuous capsules (`9999px` / pill-shaped) to represent live transient states, live pulses, and toggle hit-targets.

## Components

### 1. Master Takeover Switch
The panel's primary safety control. Positioned in the prominent header pod:
- **Off State (Agent Running)**: Subtle translucent capsule ring with label "Agent Autonomous".
- **Engaged State (Takeover Active)**: Radiates a high-visibility amber/red breathing border highlight (`box-shadow: 0 0 16px rgba(255, 69, 58, 0.4)`), immediately locking browser automation and giving direct human keyboard/pointer control.

### 2. Status Capsule & Pulse Indicator
- Located alongside the bridge session ID.
- Displays connection state via a 6px circular LED with an animated radial ping (`@keyframes ripple`). Green indicates active WebSockets relay; red denotes bridge detachment; yellow signals unhandled approvals.

### 3. Segmented Navigation Bar
- visionOS floating pill slider: A capsule pill track (`rgba(0, 0, 0, 0.25)`) featuring an active sliding glass thumb (`rgba(255, 255, 255, 0.14)`) that shifts with elastic ease across tabs (`Approvals`, `Origins`, `Blocklist`, `Activity`). Badge counters appear in high-contrast miniature discs.

### 4. Approval Queue Cards
Each pending agent request renders inside an isolated glass tile:
- **Card Header**: Action intent badge (e.g., `DOM Mutation`, `Cookie Access`, `Foreign Navigation`) alongside time elapsed.
- **Target Origin Box**: Monospaced terminal-style container highlighting domain name and URL parameter payload.
- **Action Button Triad**:
  - `Reject`: Translucent tertiary wash with red typography.
  - `Allow Once`: Frosted neutral button with subtle highlight.
  - `Always Allow`: Vivid emerald fill (`#30D158`) with crisp white contrast text.

### 5. Origins Whitelist & Paired State Rows
- **Origin Rows**: List item row featuring a favicon slot, truncated SSL domain, permissions badge, and an unobtrusive glass "Revoke" button on hover.
- **Pairing Tray**: Visual lock status displaying current core daemon handshake (`bridge-core v2.4.1`) with a one-click "Re-pair" glass chip.
