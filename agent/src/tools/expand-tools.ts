import { defineTool } from '@flue/runtime';
import * as v from 'valibot';
import { db } from '../db/client';
import { planeClient } from '../plane/client';
import { planeStateMap } from '../plane/state-map';
import { type Task, type TaskType } from '../types';

const LABEL_COLORS: Record<TaskType, string> = {
  FEAT: 'Azul',
  FIX: 'Rojo',
  HOTFIX: 'Naranja',
  TEST: 'Amarillo',
  EPIC: 'Morado',
};

const DEFAULT_TECH_TABLE = `| Área | Impacto |
|------|---------|
| Backend | Por definir |
| Frontend | Por definir |
| Base de datos | Por definir |`;

const ACTIVITY_FEAT = `- [ ] Asignar responsable
- [ ] Revisión técnica
- [ ] Implementación
- [ ] QA / Testing
- [ ] Deploy a producción`;

const ACTIVITY_BUG = `- [ ] Reproducir el bug
- [ ] Identificar causa raíz
- [ ] Implementar fix
- [ ] Testing de regresión
- [ ] Deploy a producción`;

const ACTIVITY_TEST = `- [ ] Definir casos de prueba
- [ ] Implementar tests
- [ ] Revisar cobertura
- [ ] Documentar resultados`;

function buildDescription(
  task: Task,
  objective: string,
  justification: string,
  technicalNotes?: string,
  extraContext?: string,
): string {
  const title = `${task.type} - ${task.consecutive} - ${task.name}`;
  const tech = technicalNotes ?? DEFAULT_TECH_TABLE;
  const ctx = extraContext ?? 'N/A';

  let functionalSection: string;
  let activitySection: string;

  if (task.type === 'FEAT' || task.type === 'EPIC') {
    functionalSection = `## Descripción Funcional
### Historia de Usuario
| Campo | Detalle |
|-------|---------|
| Como | Usuario del sistema |
| Quiero | ${task.name} |
| Para | ${objective} |
| Pre-Condiciones | ${ctx} |
| Criterios de aceptación | Funcionalidad implementada según lo especificado |`;
    activitySection = ACTIVITY_FEAT;
  } else if (task.type === 'FIX' || task.type === 'HOTFIX') {
    functionalSection = `## Descripción Funcional
### Reporte de Bug
| Campo | Detalle |
|-------|---------|
| Funcionalidad Relacionada | ${task.name} |
| Ambiente | Producción |
| Pasos a Reproducir | ${ctx} |
| Resultado Actual | Ver descripción del issue |
| Resultado Esperado | ${objective} |`;
    activitySection = ACTIVITY_BUG;
  } else {
    // TEST
    functionalSection = `## Descripción Funcional
### Plan de Verificación
| Campo | Detalle |
|-------|---------|
| Funcionalidad a verificar | ${task.name} |
| Objetivo de prueba | ${objective} |
| Plan de pruebas | ${ctx} |
| Criterios de éxito | ${justification} |`;
    activitySection = ACTIVITY_TEST;
  }

  return `# ${title}

## Esquema de Nomenclatura
**${title}**

## Objetivo y Justificación
| Campo | Detalle |
|-------|---------|
| Objetivo | ${objective} |
| Justificación | ${justification} |

${functionalSection}

## Consideraciones Técnicas
${tech}

## Anexos
_Sin anexos por el momento._

## Actividad
${activitySection}`;
}

export async function doExpandTask(input: {
  id: number;
  project_slug?: string;
  objective: string;
  justification: string;
  technical_notes?: string;
  extra_context?: string;
}): Promise<{ plane_url: string; issue_id: string }> {
  const task = db.prepare('SELECT * FROM tasks WHERE id = ?').get(input.id) as Task | undefined;
  if (!task) throw new Error(`Task ${input.id} not found`);

  const projectSlug = input.project_slug ?? planeClient.defaultProjectSlug;
  if (!projectSlug) throw new Error('project_slug is required (or set PLANE_DEFAULT_PROJECT_SLUG)');

  const title = `${task.type} - ${task.consecutive} - ${task.name}`;
  const description = buildDescription(
    task,
    input.objective,
    input.justification,
    input.technical_notes,
    input.extra_context,
  );

  const stateId = await planeClient.getStateId(projectSlug, planeStateMap['todo']!);
  const issueId = await planeClient.createIssue(
    projectSlug,
    title,
    description,
    stateId,
    [LABEL_COLORS[task.type]],
  );

  db.prepare('UPDATE tasks SET plane_issue_id = ?, plane_project_slug = ? WHERE id = ?').run(
    issueId,
    projectSlug,
    input.id,
  );

  const baseUrl = (process.env.PLANE_BASE_URL ?? '').replace(/\/$/, '');
  const workspaceSlug = process.env.PLANE_WORKSPACE_SLUG ?? '';
  return { plane_url: `${baseUrl}/${workspaceSlug}/projects/${projectSlug}/issues/${issueId}`, issue_id: issueId };
}

export const expandTaskTool = defineTool({
  name: 'expand_task',
  description: 'Generate a full Plane.io issue from a local task and link them together',
  input: v.object({
    id: v.number(),
    project_slug: v.optional(v.string()),
    objective: v.string(),
    justification: v.string(),
    technical_notes: v.optional(v.string()),
    extra_context: v.optional(v.string()),
  }),
  output: v.object({ plane_url: v.string(), issue_id: v.string() }),
  async run({ input }) {
    return doExpandTask(input);
  },
});
