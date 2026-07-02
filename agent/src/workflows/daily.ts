import '../app';
import '../db/schema';
import { defineWorkflow, defineAgent } from '@flue/runtime';
import * as v from 'valibot';
import { createDaily, pushDaily } from '../tools/daily-tools';

export default defineWorkflow({
  agent: defineAgent(() => ({
    model: `kimi/${process.env.KIMI_MODEL ?? 'kimi-k2.7-code'}`,
    instructions: 'Daily report generation workflow agent',
  })),
  input: v.object({
    date: v.optional(v.string()),
    exclude_ids: v.optional(v.array(v.number())),
    modules: v.optional(v.array(v.string())),
    push: v.optional(v.boolean()),
  }),
  output: v.object({
    markdown: v.string(),
    daily_id: v.number(),
    pushed: v.boolean(),
  }),
  async run({ input }) {
    const { daily_id, markdown } = await createDaily({
      date: input.date,
      exclude_ids: input.exclude_ids,
      modules: input.modules,
    });

    let pushed = false;
    if (input.push) {
      await pushDaily(daily_id);
      pushed = true;
    }

    return { markdown, daily_id, pushed };
  },
});
