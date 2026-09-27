import { type KeyboardEvent, useRef } from 'react';
import type { SidePanelTab } from '../../side-panel-state';
import styles from './SegmentedNav.module.css';

export interface SegmentedTab {
  id: SidePanelTab;
  label: string;
  badge?: number;
}

interface SegmentedNavProps {
  tabs: SegmentedTab[];
  // null = no tab is active yet (the panel is still loading). The nav
  // renders without any tab selected — the user can still click to pick
  // one, and once `activeTab` becomes non-null the corresponding tab
  // highlights.
  activeTab: SidePanelTab | null;
  onActivate: (tab: SidePanelTab) => void;
}

// visionOS-style segmented tab bar. Roving tabindex per the WAI-ARIA tabs
// pattern: only the active tab is in the tab order; arrow keys move between
// tabs, Home/End jump to the ends.
export function SegmentedNav({
  tabs,
  activeTab,
  onActivate,
}: SegmentedNavProps) {
  const buttonRefs = useRef<Map<SidePanelTab, HTMLButtonElement>>(new Map());

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>): void => {
    if (
      event.key !== 'ArrowLeft' &&
      event.key !== 'ArrowRight' &&
      event.key !== 'Home' &&
      event.key !== 'End'
    ) {
      return;
    }
    const currentIndex = tabs.findIndex((tab) => tab.id === activeTab);
    if (currentIndex < 0) return;
    let nextIndex = currentIndex;
    if (event.key === 'ArrowLeft')
      nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
    if (event.key === 'ArrowRight')
      nextIndex = (currentIndex + 1) % tabs.length;
    if (event.key === 'Home') nextIndex = 0;
    if (event.key === 'End') nextIndex = tabs.length - 1;
    const nextTab = tabs[nextIndex];
    event.preventDefault();
    onActivate(nextTab.id);
    buttonRefs.current.get(nextTab.id)?.focus();
  };

  return (
    <div
      className={styles.nav}
      role="tablist"
      aria-label="Side panel sections"
      onKeyDown={handleKeyDown}
    >
      {tabs.map((tab) => {
        const isActive = tab.id === activeTab;
        return (
          <button
            key={tab.id}
            ref={(element) => {
              if (element) buttonRefs.current.set(tab.id, element);
              else buttonRefs.current.delete(tab.id);
            }}
            type="button"
            role="tab"
            id={`tab-${tab.id}`}
            aria-selected={isActive ? 'true' : 'false'}
            aria-controls={`panel-${tab.id}`}
            tabIndex={isActive ? 0 : -1}
            className={`${styles.tab}${isActive ? ` ${styles.active}` : ''}`}
            onClick={() => onActivate(tab.id)}
          >
            <span>{tab.label}</span>
            {tab.badge !== undefined && tab.badge > 0 && (
              <span className={styles.badge}>{tab.badge}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}
