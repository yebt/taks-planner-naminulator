import { defineTool } from '@flue/runtime';
import * as v from 'valibot';
import { db } from '../db/client';
import { sendTelegram } from '../telegram/client';
import { dailySchema, type Task, type Daily } from '../types';

// MarkdownV2 requires escaping: _ * [ ] ( ) ~ ` > # + - = | { } . !
function escapeMdV2(text: string): string {
  return text.replace(/[_*[\]()~`>#+\-=|{}.!]/g, '\\$&');
}

function buildMarkdown(date: string, tasks: Task[]): string {
  const byModule = new Map<string, Task[]>();
  for (const task of tasks) {
    if (!byModule.has(task.module)) byModule.set(task.module, []);
    byModule.get(task.module)!.push(task);
  }

  const lines: string[] = [`📅 *Daily — ${escapeMdV2(date)}*`, ''];

  for (const [module, moduleTasks] of byModule) {
    lines.push(`*${escapeMdV2(module)}*`);
    for (const task of moduleTasks) {
      lines.push(
        `• \\[${escapeMdV2(task.type)}\\] ${escapeMdV2(String(task.consecutive))}\\. ${escapeMdV2(task.name)} — _${escapeMdV2(task.status)}_`,
      );
    }
    lines.push('');
  }

  lines.push(`📊 Total: ${tasks.length} tasks`);
  return lines.join('\n');
}

function rowToDaily(row: Record<string, unknown>): Daily {
  return {
    id: row['id'] as number,
    date: row['date'] as string,
    task_ids: JSON.parse(row['task_ids'] as string) as number[],
    markdown: row['markdown'] as string,
    telegram_pushed: row['telegram_pushed'] === 1,
    created_at: row['created_at'] as string,
  };
}

export async function createDaily(input: {
  date?: string;
  exclude_ids?: number[];
  modules?: string[];
}): Promise<{ daily_id: number; markdown: string }> {
  const date = input.date ?? new Date().toISOString().slice(0, 10);
  let query = "SELECT * FROM tasks WHERE status NOT IN ('done', 'cancelled')";
  const params: unknown[] = [];

  if (input.modules?.length) {
    query += ` AND module IN (${input.modules.map(() => '?').join(', ')})`;
    params.push(...input.modules);
  }
  if (input.exclude_ids?.length) {
    query += ` AND id NOT IN (${input.exclude_ids.map(() => '?').join(', ')})`;
    params.push(...input.exclude_ids);
  }

  query += ' ORDER BY module, consecutive';
  const tasks = db.prepare(query).all(...params) as Task[];
  const taskIds = tasks.map((t) => t.id);
  const markdown = buildMarkdown(date, tasks);

  const result = db
    .prepare('INSERT INTO dailies (date, task_ids, markdown) VALUES (?, ?, ?)')
    .run(date, JSON.stringify(taskIds), markdown);

  return { daily_id: Number(result.lastInsertRowid), markdown };
}

export async function pushDaily(daily_id: number): Promise<void> {
  const row = db.prepare('SELECT * FROM dailies WHERE id = ?').get(daily_id) as Record<string, unknown> | undefined;
  if (!row) throw new Error(`Daily ${daily_id} not found`);
  const daily = rowToDaily(row);
  await sendTelegram(daily.markdown);
  db.prepare('UPDATE dailies SET telegram_pushed = 1 WHERE id = ?').run(daily_id);
}

export const createDailyTool = defineTool({
  name: 'create_daily',
  description: 'Generate a daily report markdown from active tasks, save it, and return the result',
  input: v.object({
    date: v.optional(v.string()),
    exclude_ids: v.optional(v.array(v.number())),
    modules: v.optional(v.array(v.string())),
  }),
  output: v.object({ daily_id: v.number(), markdown: v.string() }),
  async run({ input }) {
    return createDaily(input);
  },
});

export const getDailyTool = defineTool({
  name: 'get_daily',
  description: 'Get all daily reports for a given date (YYYY-MM-DD)',
  input: v.object({ date: v.string() }),
  output: v.object({ dailies: v.array(dailySchema) }),
  async run({ input }) {
    const rows = db.prepare('SELECT * FROM dailies WHERE date = ? ORDER BY id DESC').all(input.date) as Record<string, unknown>[];
    return { dailies: rows.map(rowToDaily) };
  },
});

export const pushDailyTool = defineTool({
  name: 'push_daily',
  description: 'Send a daily report to Telegram and mark it as pushed',
  input: v.object({ daily_id: v.number() }),
  output: v.object({ success: v.boolean() }),
  async run({ input }) {
    await pushDaily(input.daily_id);
    return { success: true };
  },
});
