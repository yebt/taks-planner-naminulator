import '../app';
import '../db/schema';
import { defineWorkflow, defineAgent } from '@flue/runtime';
import * as v from 'valibot';
import { applyStatus, doAddComment } from '../tools/status-tools';

export default defineWorkflow({
  agent: defineAgent(() => ({
    model: `kimi/${process.env.KIMI_MODEL ?? 'kimi-k2.7-code'}`,
    instructions: 'Task management workflow agent',
  })),
  input: v.object({
    id: v.number(),
    action: v.picklist(['done', 'in_progress', 'paused', 'cancelled'] as const),
    comment: v.optional(v.string()),
  }),
  output: v.object({ success: v.boolean(), message: v.string() }),
  async run({ input }) {
    const { id, action, comment } = input;

    await applyStatus(id, action);

    if (comment) {
      await doAddComment(id, comment);
    }

    const labels: Record<string, string> = {
      done: 'marked as done',
      in_progress: 'marked as in progress',
      paused: 'paused',
      cancelled: 'cancelled',
    };

    const commentNote = comment ? ' Comment added.' : '';
    return {
      success: true,
      message: `Task ${id} ${labels[action] ?? action}.${commentNote}`,
    };
  },
});
