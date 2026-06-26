import { defineTool } from '@flue/runtime';
import * as v from 'valibot';
import { db } from '../db/client';
import { planeClient } from '../plane/client';
import { planeStateMap } from '../plane/state-map';
import { taskSchema, type Task } from '../types';

async function setStatus(id: number, status: string): Promise<Task> {
  db.prepare('UPDATE tasks SET status = ? WHERE id = ?').run(status, id);
  const task = db.prepare('SELECT * FROM tasks WHERE id = ?').get(id) as Task | undefined;
  if (!task) throw new Error(`Task ${id} not found`);
  return task;
}

async function syncToPlane(task: Task): Promise<void> {
  if (task.plane_issue_id && task.plane_project_slug) {
    await planeClient.updateIssueState(
      task.plane_project_slug,
      task.plane_issue_id,
      planeStateMap[task.status]!,
    );
  }
}

export async function applyStatus(
  id: number,
  status: 'todo' | 'in_progress' | 'paused' | 'done' | 'cancelled',
): Promise<Task> {
  const task = await setStatus(id, status);
  await syncToPlane(task);
  return task;
}

export async function doAddComment(
  id: number,
  comment: string,
): Promise<{ success: boolean; synced: boolean }> {
  const task = db.prepare('SELECT * FROM tasks WHERE id = ?').get(id) as Task | undefined;
  if (!task) throw new Error(`Task ${id} not found`);
  let synced = false;
  if (task.plane_issue_id && task.plane_project_slug) {
    await planeClient.addComment(task.plane_project_slug, task.plane_issue_id, comment);
    synced = true;
  }
  return { success: true, synced };
}

export const markDoneTool = defineTool({
  name: 'mark_done',
  description: 'Mark a task as done and sync its state to Plane if connected',
  input: v.object({ id: v.number() }),
  output: v.object({ task: taskSchema }),
  async run({ input }) {
    const task = await setStatus(input.id, 'done');
    await syncToPlane(task);
    return { task };
  },
});

export const markInProgressTool = defineTool({
  name: 'mark_in_progress',
  description: 'Mark a task as in_progress and sync its state to Plane if connected',
  input: v.object({ id: v.number() }),
  output: v.object({ task: taskSchema }),
  async run({ input }) {
    const task = await setStatus(input.id, 'in_progress');
    await syncToPlane(task);
    return { task };
  },
});

export const pauseTaskTool = defineTool({
  name: 'pause_task',
  description: 'Mark a task as paused and sync its state to Plane if connected',
  input: v.object({ id: v.number() }),
  output: v.object({ task: taskSchema }),
  async run({ input }) {
    const task = await setStatus(input.id, 'paused');
    await syncToPlane(task);
    return { task };
  },
});

export const cancelTaskTool = defineTool({
  name: 'cancel_task',
  description: 'Mark a task as cancelled and sync its state to Plane if connected',
  input: v.object({ id: v.number() }),
  output: v.object({ task: taskSchema }),
  async run({ input }) {
    const task = await setStatus(input.id, 'cancelled');
    await syncToPlane(task);
    return { task };
  },
});

export const addCommentTool = defineTool({
  name: 'add_comment',
  description: 'Add a comment to a task and sync it to Plane if connected',
  input: v.object({ id: v.number(), comment: v.string() }),
  output: v.object({ success: v.boolean(), synced: v.boolean() }),
  async run({ input }) {
    const task = db.prepare('SELECT * FROM tasks WHERE id = ?').get(input.id) as Task | undefined;
    if (!task) throw new Error(`Task ${input.id} not found`);

    let synced = false;
    if (task.plane_issue_id && task.plane_project_slug) {
      await planeClient.addComment(task.plane_project_slug, task.plane_issue_id, input.comment);
      synced = true;
    }

    return { success: true, synced };
  },
});
