import { db } from './client';

export function initSchema(): void {
  db.exec(`
    CREATE TABLE IF NOT EXISTS tasks (
      id                INTEGER PRIMARY KEY AUTOINCREMENT,
      type              TEXT NOT NULL CHECK(type IN ('FEAT', 'FIX', 'HOTFIX', 'TEST', 'EPIC')),
      consecutive       INTEGER NOT NULL UNIQUE,
      name              TEXT NOT NULL,
      module            TEXT NOT NULL,
      status            TEXT NOT NULL DEFAULT 'todo'
                          CHECK(status IN ('todo', 'in_progress', 'paused', 'done', 'cancelled')),
      priority          TEXT NOT NULL DEFAULT 'medium'
                          CHECK(priority IN ('low', 'medium', 'high', 'urgent')),
      description       TEXT,
      plane_issue_id    TEXT,
      plane_project_slug TEXT,
      created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );

    CREATE TABLE IF NOT EXISTS dailies (
      id               INTEGER PRIMARY KEY AUTOINCREMENT,
      date             TEXT NOT NULL,
      task_ids         TEXT NOT NULL DEFAULT '[]',
      markdown         TEXT NOT NULL DEFAULT '',
      telegram_pushed  INTEGER NOT NULL DEFAULT 0,
      created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
  `);
}

initSchema();
