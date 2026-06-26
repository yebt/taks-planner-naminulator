export const planeStateMap: Record<string, string> = {
  todo: process.env.PLANE_STATE_TODO ?? 'Unstarted',
  in_progress: process.env.PLANE_STATE_IN_PROGRESS ?? 'Started',
  paused: process.env.PLANE_STATE_PAUSED ?? 'Backlog',
  done: process.env.PLANE_STATE_DONE ?? 'Completed',
  cancelled: process.env.PLANE_STATE_CANCELLED ?? 'Cancelled',
};
