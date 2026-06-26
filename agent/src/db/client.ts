import Database from 'better-sqlite3';
import { mkdirSync } from 'fs';
import { dirname, resolve } from 'path';

const dbPath = resolve(process.env.DB_PATH ?? './data/planner.db');
mkdirSync(dirname(dbPath), { recursive: true });

export const db = new Database(dbPath);

// WAL mode for better concurrent read performance
db.pragma('journal_mode = WAL');
