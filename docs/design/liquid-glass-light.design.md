---
name: Liquid Glass Light
colors:
  surface: '#fcf8fb'
  surface-dim: '#dcd9dc'
  surface-bright: '#fcf8fb'
  surface-container-lowest: '#ffffff'
  surface-container-low: '#f6f3f5'
  surface-container: '#f0edef'
  surface-container-high: '#eae7ea'
  surface-container-highest: '#e4e2e4'
  on-surface: '#1b1b1d'
  on-surface-variant: '#414755'
  inverse-surface: '#303032'
  inverse-on-surface: '#f3f0f2'
  outline: '#717786'
  outline-variant: '#c1c6d7'
  surface-tint: '#005bc1'
  primary: '#0058bc'
  on-primary: '#ffffff'
  primary-container: '#0070eb'
  on-primary-container: '#fefcff'
  inverse-primary: '#adc6ff'
  secondary: '#4c4aca'
  on-secondary: '#ffffff'
  secondary-container: '#6664e4'
  on-secondary-container: '#fffbff'
  tertiary: '#006b27'
  on-tertiary: '#ffffff'
  tertiary-container: '#008733'
  on-tertiary-container: '#f7fff2'
  error: '#ba1a1a'
  on-error: '#ffffff'
  error-container: '#ffdad6'
  on-error-container: '#93000a'
  primary-fixed: '#d8e2ff'
  primary-fixed-dim: '#adc6ff'
  on-primary-fixed: '#001a41'
  on-primary-fixed-variant: '#004493'
  secondary-fixed: '#e2dfff'
  secondary-fixed-dim: '#c2c1ff'
  on-secondary-fixed: '#0c006a'
  on-secondary-fixed-variant: '#3631b4'
  tertiary-fixed: '#72fe88'
  tertiary-fixed-dim: '#53e16f'
  on-tertiary-fixed: '#002107'
  on-tertiary-fixed-variant: '#00531c'
  background: '#fcf8fb'
  on-background: '#1b1b1d'
  surface-variant: '#e4e2e4'
typography:
  display-lg:
    fontFamily: Plus Jakarta Sans
    fontSize: 56px
    fontWeight: '700'
    lineHeight: 64px
    letterSpacing: -0.03em
  display-lg-mobile:
    fontFamily: Plus Jakarta Sans
    fontSize: 36px
    fontWeight: '700'
    lineHeight: 44px
    letterSpacing: -0.025em
  headline-xl:
    fontFamily: Plus Jakarta Sans
    fontSize: 40px
    fontWeight: '600'
    lineHeight: 48px
    letterSpacing: -0.025em
  headline-xl-mobile:
    fontFamily: Plus Jakarta Sans
    fontSize: 28px
    fontWeight: '600'
    lineHeight: 36px
    letterSpacing: -0.02em
  headline-lg:
    fontFamily: Plus Jakarta Sans
    fontSize: 32px
    fontWeight: '600'
    lineHeight: 40px
    letterSpacing: -0.02em
  headline-md:
    fontFamily: Plus Jakarta Sans
    fontSize: 24px
    fontWeight: '600'
    lineHeight: 32px
    letterSpacing: -0.015em
  title-lg:
    fontFamily: Inter
    fontSize: 20px
    fontWeight: '600'
    lineHeight: 28px
    letterSpacing: -0.01em
  title-md:
    fontFamily: Inter
    fontSize: 17px
    fontWeight: '600'
    lineHeight: 24px
    letterSpacing: -0.005em
  body-lg:
    fontFamily: Inter
    fontSize: 17px
    fontWeight: '400'
    lineHeight: 26px
    letterSpacing: -0.005em
  body-md:
    fontFamily: Inter
    fontSize: 15px
    fontWeight: '400'
    lineHeight: 22px
    letterSpacing: 0em
  body-sm:
    fontFamily: Inter
    fontSize: 13px
    fontWeight: '400'
    lineHeight: 18px
    letterSpacing: 0.005em
  label-md:
    fontFamily: Inter
    fontSize: 14px
    fontWeight: '500'
    lineHeight: 20px
    letterSpacing: 0.01em
  label-sm:
    fontFamily: Inter
    fontSize: 12px
    fontWeight: '600'
    lineHeight: 16px
    letterSpacing: 0.02em
  caption:
    fontFamily: Inter
    fontSize: 11px
    fontWeight: '500'
    lineHeight: 14px
    letterSpacing: 0.03em
rounded:
  sm: 0.25rem
  DEFAULT: 0.5rem
  md: 0.75rem
  lg: 1rem
  xl: 1.5rem
  full: 9999px
spacing:
  gutter: 1.5rem
  gutter-mobile: 1rem
  margin: 2.5rem
  margin-mobile: 1.25rem
  space-xs: 0.25rem
  space-sm: 0.5rem
  space-md: 1rem
  space-lg: 1.5rem
  space-xl: 2.25rem
---

## Brand & Style

This design system embodies the apex of liquid optical design: an ultra-refined, light-mode interface defined by dynamic translucent surfaces, specular glass refraction, and surgical optical clarity. The design language speaks to discerning power users, creative professionals, and luxury tech enthusiasts who demand seamless continuity between hardware and operating software. 

The emotional impact is immediate: airiness, weightless precision, crystalline cleanliness, and spatial composure. By marrying advanced frosted glassmorphism with high-legibility typography and strict Apple-tier visual ergonomics, the system resolves the tension between visual delight and operational utility. 

Key principles:
- **Optical Authenticity**: Surfaces feel physically present through multi-tier backdrop filters, soft interior edge gleams, and refractive ambient illumination.
- **Structural Clarity**: Dynamic blur matrices never compromise reading performance. Content sits with absolute clarity over milky translucent strata.
- **Electric Accentuation**: The iconic electric blue system accent provides instant navigational anchoring amidst pure crystalline structures.

## Colors

The palette is engineered around pure light diffusion, anchored by Apple System Blue (`#007AFF`) as the foundational action token. Secondary and tertiary accents provide semantic variance: Deep Indigo (`#5856D6`) for spatial links and contextual triggers, alongside Emerald Mint (`#34C759`) for affirmative system confirmations.

### Surface Color Logic
- **Canvas Base**: `#F5F5F7` at 100% opacity as the global environmental stage.
- **Liquid Base Surface**: `rgba(255, 255, 255, 0.65)` layered with `backdrop-filter: blur(28px) saturate(190%)`.
- **Raised Glass Container**: `rgba(255, 255, 255, 0.82)` with `backdrop-filter: blur(40px) saturate(200%)`.
- **Floating Overlays & Menus**: `rgba(255, 255, 255, 0.92)` with `backdrop-filter: blur(48px) saturate(210%)`.
- **Specular Rim Highlight**: `rgba(255, 255, 255, 0.70)` 1px internal border inset along top/left edges, cascading to `rgba(255, 255, 255, 0.20)` along bottom/right edges.

### Typography & Content Color Tokens
- **Label Primary**: `#1D1D1F` (high contrast, WCAG AAA compliant against blurred white matrices).
- **Label Secondary**: `rgba(29, 29, 31, 0.64)`.
- **Label Tertiary**: `rgba(29, 29, 31, 0.40)`.
- **Subtle Separators**: `rgba(0, 0, 0, 0.06)`.

## Typography

The typographical architecture is engineered for extreme legibility against refractive, semi-translucent backdrops. 

- **Headlines (`Plus Jakarta Sans`)**: Delivers geometric purity, wide tracking apertures, and crisp modern contours that maintain commanding hierarchy over dynamic glass panels.
- **Body & UI Controls (`Inter`)**: Chosen for neutral optical mechanics, high x-height, and precise kerning across dense data tables, modal dialogs, and navigation rows.

### Rendering Directives
- Always apply `-webkit-font-smoothing: antialiased` and `text-rendering: optimizeLegibility`.
- Text resting over semi-transparent glass layers must avoid drop shadows; hierarchy is created strictly through tone (`#1D1D1F` to `rgba(29, 29, 31, 0.64)`) and font-weight modulation.
- Large titles scale down aggressively on mobile viewport breakpoints via dedicated mobile headline tokens to preserve glass container margins.

## Layout & Spacing

The structural layout relies on an adaptive 12-column fluid grid system on desktop (`>1024px`) shifting to 8 columns on tablet (`768px - 1023px`) and 4 columns on mobile (`<767px`). 

Liquid Glass requires generous ambient space: dense crowding destroys the sensation of translucent depth. Margin spacing provides breathing room between the outer viewport edge and primary frosted glass containers, allowing the background canvas texture to frame active windows and cards.

### Layout Rules
- **Desktop Grid**: 12 columns, `gutter: 1.5rem` (24px), outer `margin: 2.5rem` (40px). Max layout container width: `1440px`.
- **Tablet Grid**: 8 columns, `gutter: 1.25rem` (20px), outer `margin: 2rem` (32px).
- **Mobile Grid**: 4 columns, `gutter-mobile: 1rem` (16px), outer `margin-mobile: 1.25rem` (20px). Panels stretch full width or retain safe-area padding.
- **Spatial Stacking**: Glass containers float with intentional outer gaps (`space-lg` to `space-xl`) rather than sharing hairline dividing lines. Component internals observe strict 8pt proportional increments via `space-xs` through `space-xl`.

## Elevation & Depth

Elevation is rendered through optical refraction, specular highlights, and diffused ambient illumination rather than conventional heavy drop shadows.

### Glass Stratum Hierarchy
1. **Level 0 (Environmental Canvas)**: Solid `#F5F5F7` backdrop. Zero blur, zero shadow.
2. **Level 1 (Dock & Sidebars)**: `background: rgba(255, 255, 255, 0.55)`, `backdrop-filter: blur(32px) saturate(180%)`. Border: 1px solid `rgba(255, 255, 255, 0.6)`. Shadow: `0 8px 32px 0 rgba(0, 0, 0, 0.04)`.
3. **Level 2 (Active Windows & Primary Panels)**: `background: rgba(255, 255, 255, 0.72)`, `backdrop-filter: blur(44px) saturate(190%)`. Border: 1px solid `rgba(255, 255, 255, 0.8)`. Shadow: `0 16px 40px -10px rgba(0, 0, 0, 0.06), 0 0 1px 1px rgba(255, 255, 255, 0.9) inset`.
4. **Level 3 (Modals, Popovers & Contextual Palettes)**: `background: rgba(255, 255, 255, 0.88)`, `backdrop-filter: blur(60px) saturate(200%)`. Border: 1px solid `rgba(255, 255, 255, 0.95)`. Shadow: `0 24px 64px -12px rgba(0, 0, 0, 0.12), 0 0 0 1px rgba(0, 0, 0, 0.03)`.

### Specular Highlighting
All elevated panels incorporate a top-to-bottom directional gradient border simulating overhead lighting hitting polished glass:
`linear-gradient(180deg, rgba(255, 255, 255, 0.85) 0%, rgba(255, 255, 255, 0.25) 100%)`.

## Shapes

The design system employs continuous super-ellipses (squircle geometry) to achieve smooth, biological contours fitting for liquid glass optics. Standard border-radius tokens are anchored at `roundedness: 2`:

- **Small Components (Chips, Badges, Micro-toggles)**: `0.5rem` (8px).
- **Interactive Controls (Inputs, Standard Buttons)**: `0.75rem` (12px).
- **Secondary Containers & Cards (`rounded-lg`)**: `1rem` (16px).
- **Primary Windows, Viewport Modules & Dialogs (`rounded-xl`)**: `1.5rem` (24px).
- **Full Pills**: Used selectively for segmented tab switches, search capsules, and primary floating action bars (`9999px`).

## Components

### Buttons
- **Primary**: Solid Apple System Blue (`#007AFF`) background with pure white `#FFFFFF` text. Micro subtle inner top highlight (`inset 0 1px 0 rgba(255, 255, 255, 0.25)`). Hover increases brightness by 4%; active state triggers scale down to `0.98`.
- **Secondary / Glass Button**: Translucent fill `rgba(255, 255, 255, 0.70)` with `backdrop-filter: blur(16px)`, border: 1px solid `rgba(255, 255, 255, 0.85)`, text color `#1D1D1F`. Hover transitions fill to `rgba(255, 255, 255, 0.90)`.
- **Ghost**: Zero background fill; text `#007AFF` with 14px horizontal padding; hover renders soft `rgba(0, 122, 255, 0.08)` pill fill.

### Cards & Panels
- Constructed with `Level 2` elevation tokens. Outer corner radius: `1.5rem` (24px). 
- Inner padding: `space-lg` (24px).
- Subdivided content inside cards utilizes hairline glass dividers (`1px` height, `background: rgba(0, 0, 0, 0.05)`), never solid borders.

### Input Fields
- **Default**: Pill or rounded-rectangle (`0.75rem`), background: `rgba(0, 0, 0, 0.04)`, border: `1px solid rgba(0, 0, 0, 0.06)`, text: `#1D1D1F`, placeholder: `rgba(29, 29, 31, 0.38)`.
- **Focused State**: Background shifts to `rgba(255, 255, 255, 0.95)`, border color: `#007AFF`, box-shadow: `0 0 0 3px rgba(0, 122, 255, 0.25)`.

### Chips & Tags
- Height: `28px`. Radius: `9999px`.
- Inactive: `background: rgba(255, 255, 255, 0.65)`, border: `1px solid rgba(255, 255, 255, 0.8)`, text: `rgba(29, 29, 31, 0.75)`.
- Selected: `background: #007AFF`, border: `1px solid #007AFF`, text: `#FFFFFF`.

### Checkboxes & Radio Controls
- Base: `18px × 18px`.
- Unchecked: `background: rgba(255, 255, 255, 0.65)`, border: `1.5px solid rgba(29, 29, 31, 0.25)`.
- Checked: `background: #007AFF`, border: `1.5px solid #007AFF`. Checkmark icon: `#FFFFFF` 2px stroke weight. Radio pip: `6px` solid `#FFFFFF` sphere.

### Lists & Row Items
- Row height: `48px` minimum. Hover state produces an instantaneous soft pill highlight overlay (`rgba(0, 0, 0, 0.04)`) with `8px` corner radius. 
- Leading icons sit inside `32px × 32px` rounded glass badges with subtle blue tinting (`rgba(0, 122, 255, 0.12)`).

### Glass Segmented Controls
- A unified pill housing `rgba(0, 0, 0, 0.05)` containing sliding active pill indicator `rgba(255, 255, 255, 0.95)` with `box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08)`. Selected typography flips from tertiary to primary `#1D1D1F`.
