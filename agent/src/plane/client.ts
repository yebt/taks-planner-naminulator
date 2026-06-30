interface PlaneState {
  id: string;
  name: string;
}

interface PlaneStatesResponse {
  results: PlaneState[];
}

interface PlaneIssueResponse {
  id: string;
}

export class PlaneClient {
  private readonly baseUrl: string;
  private readonly apiKey: string;
  private readonly workspaceSlug: string;
  readonly defaultProjectId: string;
  // state name (lower) → state ID, keyed by projectSlug
  private readonly stateCache = new Map<string, Map<string, string>>();

  constructor() {
    this.baseUrl = (process.env.PLANE_BASE_URL ?? '').replace(/\/$/, '');
    this.apiKey = process.env.PLANE_API_TOKEN ?? '';
    this.workspaceSlug = process.env.PLANE_WORKSPACE_SLUG ?? '';
    this.defaultProjectId = process.env.PLANE_DEFAULT_PROJECT_ID ?? '';
  }

  private headers(): Record<string, string> {
    return {
      'X-Api-Key': this.apiKey,
      'Content-Type': 'application/json',
    };
  }

  private apiUrl(path: string): string {
    return `${this.baseUrl}/api/v1/workspaces/${this.workspaceSlug}${path}`;
  }

  async getStateId(projectSlug: string, stateName: string): Promise<string> {
    if (!this.stateCache.has(projectSlug)) {
      const res = await fetch(this.apiUrl(`/projects/${projectSlug}/states/`), {
        headers: this.headers(),
      });
      if (!res.ok) throw new Error(`Failed to fetch states: ${res.status}`);
      const data = (await res.json()) as PlaneStatesResponse;
      const map = new Map(data.results.map((s) => [s.name.toLowerCase(), s.id]));
      this.stateCache.set(projectSlug, map);
    }
    const stateId = this.stateCache.get(projectSlug)!.get(stateName.toLowerCase());
    if (!stateId) throw new Error(`Plane state not found: "${stateName}" in project "${projectSlug}"`);
    return stateId;
  }

  async createIssue(
    projectSlug: string,
    title: string,
    description: string,
    stateId: string,
    labelNames?: string[],
  ): Promise<string> {
    const body: Record<string, unknown> = {
      name: title,
      description_html: `<p>${description.replace(/\n/g, '<br>')}</p>`,
      state: stateId,
    };
    if (labelNames?.length) {
      body.label_ids = labelNames;
    }

    const res = await fetch(this.apiUrl(`/projects/${projectSlug}/issues/`), {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error(`Failed to create issue: ${res.status} ${await res.text()}`);
    const data = (await res.json()) as PlaneIssueResponse;
    return data.id;
  }

  async updateIssueState(projectSlug: string, issueId: string, stateName: string): Promise<void> {
    const stateId = await this.getStateId(projectSlug, stateName);
    const res = await fetch(this.apiUrl(`/projects/${projectSlug}/issues/${issueId}/`), {
      method: 'PATCH',
      headers: this.headers(),
      body: JSON.stringify({ state: stateId }),
    });
    if (!res.ok) throw new Error(`Failed to update issue state: ${res.status}`);
  }

  async addComment(projectSlug: string, issueId: string, comment: string): Promise<void> {
    const res = await fetch(this.apiUrl(`/projects/${projectSlug}/issues/${issueId}/comments/`), {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify({ comment_html: `<p>${comment}</p>` }),
    });
    if (!res.ok) throw new Error(`Failed to add comment: ${res.status}`);
  }
}

export const planeClient = new PlaneClient();
