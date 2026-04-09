<template>
  <div class="copilot-page">
    <n-card title="🤖 策略 Copilot">
      <div class="chat-container">
        <div class="messages" ref="messagesRef">
          <div v-for="(msg, i) in messages" :key="i" :class="['msg', msg.role]">
            <div class="msg-bubble" v-html="formatMessage(msg.content)"></div>
            <div v-if="msg.code" class="code-block">
              <div class="code-header">
                <span>{{ msg.language || 'go' }}</span>
                <n-button size="tiny" quaternary @click="copyCode(msg.code)">复制</n-button>
              </div>
              <pre><code>{{ msg.code }}</code></pre>
            </div>
          </div>
          <div v-if="!messages.length" class="empty-chat">
            <p>描述你想要实现的策略，例如：</p>
            <ul>
              <li>"写一个基于 RSI 的均值回归策略"</li>
              <li>"实现一个双均线交叉策略，参数可调"</li>
              <li>"写一个基于布林带的突破策略"</li>
            </ul>
          </div>
        </div>
        <div class="input-area">
          <n-input
            v-model:value="inputText"
            type="textarea"
            :placeholder="'描述你想要实现的策略...'"
            :autosize="{ minRows: 2, maxRows: 4 }"
            @keydown.enter.exact="sendMessage"
          />
          <n-button type="primary" :disabled="!inputText.trim() || generating" :loading="generating" @click="sendMessage">
            ➤ 发送
          </n-button>
        </div>
      </div>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { NCard, NInput, NButton, useMessage } from 'naive-ui'
import { generateStrategy } from '@/api/copilot'

const message = useMessage()
const inputText = ref('')
const generating = ref(false)
const messagesRef = ref<HTMLElement>()
const messages = ref<Array<{ role: string; content: string; code?: string; language?: string }>>([])

async function sendMessage() {
  const text = inputText.value.trim()
  if (!text || generating.value) return

  messages.value.push({ role: 'user', content: text })
  inputText.value = ''
  await nextTick()
  scrollToBottom()

  generating.value = true
  messages.value.push({ role: 'assistant', content: '正在生成策略...' })

  try {
    const res = await generateStrategy({ prompt: text })
    const lastMsg = messages.value[messages.value.length - 1]
    lastMsg.content = res.explanation || '策略已生成'
    lastMsg.code = res.code
    lastMsg.language = res.language
  } catch (e: any) {
    const lastMsg = messages.value[messages.value.length - 1]
    lastMsg.content = `生成失败: ${e.message}`
  } finally {
    generating.value = false
    await nextTick()
    scrollToBottom()
  }
}

function formatMessage(content: string): string {
  return content.replace(/\n/g, '<br>')
}

function copyCode(code: string) {
  navigator.clipboard.writeText(code).then(() => message.success('已复制')).catch(() => {
    const textarea = document.createElement('textarea')
    textarea.value = code
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    document.body.removeChild(textarea)
    message.success('已复制')
  })
}

function scrollToBottom() {
  if (messagesRef.value) {
    messagesRef.value.scrollTop = messagesRef.value.scrollHeight
  }
}
</script>

<style scoped>
.copilot-page { max-width: 900px; margin: 0 auto; }

.chat-container { display: flex; flex-direction: column; height: calc(100vh - 200px); }

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.msg { max-width: 85%; }
.msg.user { align-self: flex-end; }
.msg.assistant { align-self: flex-start; }

.msg-bubble {
  padding: 12px 16px;
  border-radius: var(--q-radius);
  font-size: 14px;
  line-height: 1.6;
}
.msg.user .msg-bubble { background: var(--q-primary-dark); color: #fff; border-radius: var(--q-radius-sm) var(--q-radius-sm) var(--q-radius-sm) var(--q-radius); }
.msg.assistant .msg-bubble { background: var(--q-surface2); border: 1px solid var(--q-border); border-radius: var(--q-radius) var(--q-radius) var(--q-radius-sm) var(--q-radius); }

.code-block { margin-top: 8px; border: 1px solid var(--q-border); border-radius: var(--q-radius-sm); overflow: hidden; }
.code-header {
  display: flex; justify-content: space-between; align-items: center;
  padding: 6px 12px; background: var(--q-surface3); font-size: 11px; color: var(--q-text3);
}
.code-block pre { margin: 0; padding: 12px; background: var(--q-bg); overflow-x: auto; font-family: var(--q-mono); font-size: 12px; line-height: 1.6; }

.empty-chat { color: var(--q-text3); font-size: 14px; padding: 40px 20px; }
.empty-chat ul { margin-top: 8px; padding-left: 20px; }
.empty-chat li { margin: 4px 0; }

.input-area {
  display: flex; gap: 8px; padding: 12px 16px;
  border-top: 1px solid var(--q-border); background: var(--q-surface);
}
.input-area .n-input { flex: 1; }
</style>
