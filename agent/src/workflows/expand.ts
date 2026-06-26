import '../app';
import '../db/schema';
import { defineWorkflow, defineAgent } from '@flue/runtime';
import * as v from 'valibot';
import { doExpandTask } from '../tools/expand-tools';

export default defineWorkflow({
  agent: defineAgent(() => ({
    model: 'openrouter/moonshotai/kimi-k2.6',
    instructions: 'Task expansion to Plane.io workflow agent',
  })),
  input: v.object({
    id: v.number(),
    project_slug: v.optional(v.string()),
    objective: v.string(),
    justification: v.string(),
    technical_notes: v.optional(v.string()),
    extra_context: v.optional(v.string()),
  }),
  output: v.object({
    plane_url: v.string(),
    issue_id: v.string(),
  }),
  async run({ input }) {
    const { plane_url, issue_id } = await doExpandTask(input);
    return { plane_url, issue_id };
  },
});
