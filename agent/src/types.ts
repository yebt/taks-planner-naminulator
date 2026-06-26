import * as v from 'valibot';

export type TaskType = 'FEAT' | 'FIX' | 'HOTFIX' | 'TEST' | 'EPIC';
export type TaskStatus = 'todo' | 'in_progress' | 'paused' | 'done' | 'cancelled';
export type TaskPriority = 'low' | 'medium' | 'high' | 'urgent';

export interface Task {
  id: number;
  type: TaskType;
  consecutive: number;
  name: string;
  module: string;
  status: TaskStatus;
  priority: TaskPriority;
  description: string | null;
  plane_issue_id: string | null;
  plane_project_slug: string | null;
  created_at: string;
}

export interface Daily {
  id: number;
  date: string;
  task_ids: number[];
  markdown: string;
  telegram_pushed: boolean;
  created_at: string;
}

export const taskSchema = v.object({
  id: v.number(),
  type: v.picklist(['FEAT', 'FIX', 'HOTFIX', 'TEST', 'EPIC'] as const),
  consecutive: v.number(),
  name: v.string(),
  module: v.string(),
  status: v.picklist(['todo', 'in_progress', 'paused', 'done', 'cancelled'] as const),
  priority: v.picklist(['low', 'medium', 'high', 'urgent'] as const),
  description: v.nullable(v.string()),
  plane_issue_id: v.nullable(v.string()),
  plane_project_slug: v.nullable(v.string()),
  created_at: v.string(),
});

export const dailySchema = v.object({
  id: v.number(),
  date: v.string(),
  task_ids: v.array(v.number()),
  markdown: v.string(),
  telegram_pushed: v.boolean(),
  created_at: v.string(),
});
