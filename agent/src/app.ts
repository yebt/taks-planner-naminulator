import { registerProvider } from '@flue/runtime';

registerProvider('openrouter', {
  apiKey: process.env.OPENROUTER_API_KEY,
});
