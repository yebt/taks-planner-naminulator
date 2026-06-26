interface TelegramPayload {
  chat_id: string;
  text: string;
  parse_mode: 'MarkdownV2';
  reply_to_message_id?: number;
}

export async function sendTelegram(text: string): Promise<void> {
  const token = process.env.TELEGRAM_BOT_TOKEN;
  const chatId = process.env.TELEGRAM_CHAT_ID;
  if (!token || !chatId) throw new Error('TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID are required');

  const payload: TelegramPayload = {
    chat_id: chatId,
    text,
    parse_mode: 'MarkdownV2',
  };

  const threadId = process.env.TELEGRAM_THREAD_ID;
  if (threadId) {
    payload.reply_to_message_id = Number(threadId);
  }

  const res = await fetch(`https://api.telegram.org/bot${token}/sendMessage`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });

  if (!res.ok) {
    const body = await res.text();
    throw new Error(`Telegram API error ${res.status}: ${body}`);
  }
}
