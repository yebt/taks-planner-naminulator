import '../app';
import '../db/schema';
import { defineAgent } from '@flue/runtime';
import { addTaskTool, listTasksTool, getTaskTool, updateTaskTool, deleteTaskTool } from '../tools/task-tools';
import { markDoneTool, markInProgressTool, pauseTaskTool, cancelTaskTool, addCommentTool } from '../tools/status-tools';
import { createDailyTool, getDailyTool, pushDailyTool } from '../tools/daily-tools';
import { expandTaskTool } from '../tools/expand-tools';

export default defineAgent(() => ({
  model: 'openrouter/moonshotai/kimi-k2.6',
  instructions: `Eres un asistente de gestión de tareas en español. Ayudás al usuario a administrar sus tareas de desarrollo de software.

Podés:
- Agregar tareas nuevas con tipo (FEAT, FIX, HOTFIX, TEST, EPIC), módulo, prioridad y descripción
- Listar tareas con filtros opcionales por módulo, estado, tipo o cantidad
- Ver el detalle de una tarea específica
- Actualizar nombre, módulo, prioridad, descripción o estado de una tarea
- Eliminar tareas
- Cambiar el estado: marcar como hecha, en progreso, pausada o cancelada
- Agregar comentarios a tareas (se sincronizan con Plane.io si la tarea está vinculada)
- Crear reportes diarios en formato Telegram MarkdownV2
- Enviar el daily por Telegram
- Expandir una tarea a un issue completo en Plane.io con toda la documentación estructurada

Después de cada acción, explicá brevemente qué hiciste y el estado actual de la tarea o resultado.
Si el usuario pide algo ambiguo, pedí clarificación antes de actuar.
Cuando listés tareas, mostrá la información de forma clara y ordenada.`,
  tools: [
    addTaskTool,
    listTasksTool,
    getTaskTool,
    updateTaskTool,
    deleteTaskTool,
    markDoneTool,
    markInProgressTool,
    pauseTaskTool,
    cancelTaskTool,
    addCommentTool,
    createDailyTool,
    getDailyTool,
    pushDailyTool,
    expandTaskTool,
  ],
}));
