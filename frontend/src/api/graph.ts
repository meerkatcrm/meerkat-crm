// API client for network graph data
import { apiJson } from './client';
import { GraphResponse } from '../types/graph';

export async function getGraph(): Promise<GraphResponse> {
  return apiJson('/graph');
}
