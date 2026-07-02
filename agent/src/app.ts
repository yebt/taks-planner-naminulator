import { registerProvider } from '@flue/runtime';
import { flue } from '@flue/runtime/routing';

registerProvider('kimi', {
  api: 'openai',
  baseUrl: 'https://api.kimi.com/coding/v1',
  apiKey: process.env.KIMI_API_KEY,
});

export default flue();
