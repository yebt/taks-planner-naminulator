import '../app';
import '../db/schema';
import { defineWorkflow, defineAgent } from '@flue/runtime';
import * as v from 'valibot';
import { queryTasks } from '../tools/task-tools';
import { taskSchema, type Task } from '../types';

export default defineWorkflow({
  agent: defineAgent(() => ({
    model: 'openrouter/moonshotai/kimi-k2.6',
    instructions: 'Task listing workflow agent',
  })),
  input: v.object({
    module: v.optional(v.string()),
    status: v.optional(v.picklist(['todo', 'in_progress', 'paused', 'done', 'cancelled'] as const)),
    type: v.optional(v.picklist(['FEAT', 'FIX', 'HOTFIX', 'TEST', 'EPIC'] as const)),
    limit: v.optional(v.number()),
  }),
  output: v.object({ tasks: v.array(taskSchema) }),
  async run({ input }) {
    const tasks = queryTasks(input);

    if (tasks.length > 0) {
      const header = ['ID', 'Type', 'Cons.', 'Module', 'Name', 'Status', 'Priority']
        .map((h) => h.padEnd(12))
        .join('| ');
      const rows = tasks.map((t: Task) =>
        [
          String(t.id),
          t.type,
          String(t.consecutive),
          t.module,
          t.name.slice(0, 30),
          t.status,
          t.priority,
        ]
          .map((c) => c.padEnd(12))
          .join('| '),
      );
      console.log('\n' + header + '\n' + '-'.repeat(header.length));
      rows.forEach((r) => console.log(r));
      console.log(`\nTotal: ${tasks.length} task(s)\n`);
    } else {
      console.log('No tasks found.');
    }

    return { tasks };
  },
});
