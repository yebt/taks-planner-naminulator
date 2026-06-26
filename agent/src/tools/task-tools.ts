import { defineTool } from '@flue/runtime';
import * as v from 'valibot';
import { db } from '../db/client';
import { taskSchema, type Task } from '../types';

export const addTaskTool = defineTool({
  name: 'add_task',
  description: 'Create a new task with auto-assigned consecutive number',
  input: v.object({
    type: v.picklist(['FEAT', 'FIX', 'HOTFIX', 'TEST', 'EPIC'] as const),
    name: v.string(),
    module: v.string(),
    priority: v.optional(v.picklist(['low', 'medium', 'high', 'urgent'] as const)),
    description: v.optional(v.string()),
  }),
  output: v.object({ task: taskSchema }),
  async run({ input }) {
    const stmt = db.prepare(`
      INSERT INTO tasks (type, consecutive, name, module, priority, description)
      VALUES (?, (SELECT COALESCE(MAX(consecutive), 0) + 1 FROM tasks), ?, ?, ?, ?)
    `);
    const result = stmt.run(
      input.type,
      input.name,
      input.module,
      input.priority ?? 'medium',
      input.description ?? null,
    );
    const task = db.prepare('SELECT * FROM tasks WHERE id = ?').get(Number(result.lastInsertRowid)) as Task;
    return { task };
  },
});

export function queryTasks(input: {
  module?: string;
  status?: string;
  type?: string;
  limit?: number;
}): Task[] {
  let query = 'SELECT * FROM tasks WHERE 1=1';
  const params: unknown[] = [];

  if (input.module) { query += ' AND module = ?'; params.push(input.module); }
  if (input.status) { query += ' AND status = ?'; params.push(input.status); }
  if (input.type)   { query += ' AND type = ?';   params.push(input.type); }

  query += ' ORDER BY consecutive DESC';

  if (input.limit) { query += ' LIMIT ?'; params.push(input.limit); }

  return db.prepare(query).all(...params) as Task[];
}

export const listTasksTool = defineTool({
  name: 'list_tasks',
  description: 'List tasks with optional filters by module, status, type, and limit',
  input: v.object({
    module: v.optional(v.string()),
    status: v.optional(v.picklist(['todo', 'in_progress', 'paused', 'done', 'cancelled'] as const)),
    type: v.optional(v.picklist(['FEAT', 'FIX', 'HOTFIX', 'TEST', 'EPIC'] as const)),
    limit: v.optional(v.number()),
  }),
  output: v.object({ tasks: v.array(taskSchema) }),
  async run({ input }) {
    return { tasks: queryTasks(input) };
  },
});

export const getTaskTool = defineTool({
  name: 'get_task',
  description: 'Get a single task by ID',
  input: v.object({ id: v.number() }),
  output: v.object({ task: v.nullable(taskSchema) }),
  async run({ input }) {
    const task = db.prepare('SELECT * FROM tasks WHERE id = ?').get(input.id) as Task | undefined;
    return { task: task ?? null };
  },
});

export const updateTaskTool = defineTool({
  name: 'update_task',
  description: 'Update task fields (name, module, priority, description, status)',
  input: v.object({
    id: v.number(),
    name: v.optional(v.string()),
    module: v.optional(v.string()),
    priority: v.optional(v.picklist(['low', 'medium', 'high', 'urgent'] as const)),
    description: v.optional(v.string()),
    status: v.optional(v.picklist(['todo', 'in_progress', 'paused', 'done', 'cancelled'] as const)),
  }),
  output: v.object({ task: taskSchema }),
  async run({ input }) {
    const updates: string[] = [];
    const params: unknown[] = [];

    if (input.name !== undefined)        { updates.push('name = ?');        params.push(input.name); }
    if (input.module !== undefined)      { updates.push('module = ?');      params.push(input.module); }
    if (input.priority !== undefined)    { updates.push('priority = ?');    params.push(input.priority); }
    if (input.description !== undefined) { updates.push('description = ?'); params.push(input.description); }
    if (input.status !== undefined)      { updates.push('status = ?');      params.push(input.status); }

    if (updates.length > 0) {
      params.push(input.id);
      db.prepare(`UPDATE tasks SET ${updates.join(', ')} WHERE id = ?`).run(...params);
    }

    const task = db.prepare('SELECT * FROM tasks WHERE id = ?').get(input.id) as Task | undefined;
    if (!task) throw new Error(`Task ${input.id} not found`);
    return { task };
  },
});

export const deleteTaskTool = defineTool({
  name: 'delete_task',
  description: 'Delete a task by ID',
  input: v.object({ id: v.number() }),
  output: v.object({ success: v.boolean(), id: v.number() }),
  async run({ input }) {
    const info = db.prepare('DELETE FROM tasks WHERE id = ?').run(input.id);
    return { success: info.changes > 0, id: input.id };
  },
});
