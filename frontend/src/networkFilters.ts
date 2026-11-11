// Persistence for the network page's filter controls to save user selection

import type { GraphEdge } from './types/graph';

export interface NetworkFilters {
  /** Circle name to restrict the graph to; '' means "all circles". */
  selectedCircle: string;
  showRelationships: boolean;
  /** Whether ended relationships (those with an end date) are drawn; only applies while relationships are shown. */
  showEndedRelationships: boolean;
  showActivities: boolean;
  showCircles: boolean;
}

// Whether an edge stands for a relationship that has ended
export function isEndedRelationshipEdge(edge: Pick<GraphEdge, 'type' | 'end_date'>): boolean {
  return edge.type === 'relationship' && !!edge.end_date;
}

export const NETWORK_FILTERS_STORAGE_KEY = 'network-filters';

// Defaults for a user who has never touched the controls
export const DEFAULT_NETWORK_FILTERS: NetworkFilters = {
  selectedCircle: '',
  showRelationships: true,
  showEndedRelationships: true,
  showActivities: true,
  showCircles: false,
};

// Resolves the stored value into a concrete filter set
export function resolveNetworkFilters(stored: string | null | undefined): NetworkFilters {
  if (!stored) return { ...DEFAULT_NETWORK_FILTERS };
  try {
    return { ...DEFAULT_NETWORK_FILTERS, ...JSON.parse(stored) };
  } catch {
    return { ...DEFAULT_NETWORK_FILTERS };
  }
}

export function loadNetworkFilters(): NetworkFilters {
  try {
    return resolveNetworkFilters(window.localStorage.getItem(NETWORK_FILTERS_STORAGE_KEY));
  } catch {
    // localStorage can throw when disabled (e.g. Safari private mode).
    return { ...DEFAULT_NETWORK_FILTERS };
  }
}

export function saveNetworkFilters(filters: NetworkFilters): void {
  try {
    window.localStorage.setItem(NETWORK_FILTERS_STORAGE_KEY, JSON.stringify(filters));
  } catch {
    // Persistence is best-effort; a full or unavailable store must not break the page.
  }
}
