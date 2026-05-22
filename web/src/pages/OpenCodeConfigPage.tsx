import { useEffect, useState } from 'react'
import { useSettingsStore, type AvailableModel } from '@/store/settings'
import { useToastStore } from '@/store/toast'

interface OpenCodeModelConfig {
  enabled: boolean
  id: string
  name: string
  reasoning: boolean
  tool_call: boolean
  attachment: boolean
  maxInputTokens: number
  maxOutputTokens: number
}

const getDefaultModelConfig = (m: AvailableModel): OpenCodeModelConfig => {
  const id = m.model_id
  const name = m.model_name
  const idLower = id.toLowerCase()

  const isClaude = idLower.includes('claude') || idLower.includes('opus') || idLower.includes('sonnet') || idLower.includes('haiku')
  const isDeepseekReasoning = idLower.includes('deepseek-r1') || idLower.includes('reasoning')

  // Auto-detect attachment support based on supported_input_types containing "IMAGE"
  const hasImage = m.supported_input_types?.includes('IMAGE') || false
  const isAttachment = isClaude || idLower.includes('gpt-4o') || idLower.includes('gemini') || hasImage

  // Default input limits: 1M or 200k. Output limits: 4096 or 8192
  const maxInputTokens = m.token_limits?.max_input_tokens || 1000000
  const maxOutputTokens = m.token_limits?.max_output_tokens || 4096

  return {
    enabled: id !== 'auto', // disable "auto" by default since it is a proxy routing model, not a direct LLM
    id: id,
    name: name,
    reasoning: isDeepseekReasoning,
    tool_call: isClaude || idLower.includes('gpt') || idLower.includes('gemini') || idLower.includes('deepseek') || idLower.includes('qwen'),
    attachment: isAttachment,
    maxInputTokens: maxInputTokens,
    maxOutputTokens: maxOutputTokens,
  }
}

export function OpenCodeConfigPage() {
  const availableModels = useSettingsStore((s) => s.availableModels)
  const loadAvailableModels = useSettingsStore((s) => s.loadAvailableModels)
  const toast = useToastStore((s) => s.addToast)

  const [providerId, setProviderId] = useState('kiro')
  const [apiFormat, setApiFormat] = useState('openai')
  const [baseURL, setBaseURL] = useState('')
  const [apiKey, setApiKey] = useState('REPLACE_ME_PROXY')
  const [modelsList, setModelsList] = useState<OpenCodeModelConfig[]>([])
  const [defaultModelId, setDefaultModelId] = useState('')
  const [copied, setCopied] = useState(false)

  // Initialize Base URL from window.location or default
  useEffect(() => {
    if (typeof window !== 'undefined') {
      setBaseURL(`${window.location.origin}/v1`)
    } else {
      setBaseURL('http://localhost:8765/v1')
    }
  }, [])

  // Load available models on mount
  useEffect(() => {
    loadAvailableModels().catch((e) => toast(e.message, 'error'))
  }, [loadAvailableModels, toast])

  // Populate models list state when loaded
  useEffect(() => {
    if (availableModels.length > 0 && modelsList.length === 0) {
      const mapped = availableModels.map(m => getDefaultModelConfig(m))
      setModelsList(mapped)
      
      // Select the first enabled model as default
      const defaultItem = mapped.find(m => m.enabled)
      if (defaultItem) {
        setDefaultModelId(defaultItem.id)
      }
    }
  }, [availableModels, modelsList.length])

  const handleToggleModel = (id: string) => {
    setModelsList(prev => prev.map(m => {
      if (m.id === id) {
        const nextEnabled = !m.enabled
        // If we are disabling the default model, select another enabled one as default
        if (!nextEnabled && defaultModelId === id) {
          const nextDefault = prev.find(other => other.id !== id && other.enabled)
          setDefaultModelId(nextDefault ? nextDefault.id : '')
        } else if (nextEnabled && !defaultModelId) {
          setDefaultModelId(id)
        }
        return { ...m, enabled: nextEnabled }
      }
      return m
    }))
  }

  const handleModelPropChange = (id: string, prop: keyof OpenCodeModelConfig, value: any) => {
    setModelsList(prev => prev.map(m => {
      if (m.id === id) {
        return { ...m, [prop]: value }
      }
      return m
    }))
  }

  const handleCopy = () => {
    const configStr = generateJsoncString()
    navigator.clipboard.writeText(configStr).then(() => {
      setCopied(true)
      toast('Copied configuration to clipboard!', 'success')
      setTimeout(() => setCopied(false), 2000)
    }).catch((err) => {
      toast('Failed to copy: ' + err, 'error')
    })
  }

  const generateJsoncString = (): string => {
    const enabledModels = modelsList.filter(m => m.enabled)

    let jsonc = `"${providerId}": {\n`
    jsonc += `  // Giao thức API kết nối (openai hoặc anthropic)\n`
    jsonc += `  "api": "${apiFormat}",\n`
    jsonc += `  "options": {\n`
    jsonc += `    // Đường dẫn base URL của proxy server kiro-let-go\n`
    jsonc += `    "baseURL": "${baseURL}",\n`
    jsonc += `    // Khóa API của proxy dùng để xác thực (x-api-key hoặc Authorization)\n`
    jsonc += `    "apiKey": "${apiKey}"\n`
    jsonc += `  },\n`
    jsonc += `  // Bản đồ chứa các models khả dụng được cung cấp bởi proxy\n`
    jsonc += `  "models": {\n`

    enabledModels.forEach((m, idx) => {
      const isLast = idx === enabledModels.length - 1
      jsonc += `    "${m.id}": {\n`
      jsonc += `      "id": "${m.id}",\n`
      jsonc += `      "name": "${m.name}",\n`
      jsonc += `      // Bật cờ này nếu model hỗ trợ các tệp đính kèm (hình ảnh, tài liệu,...)\n`
      jsonc += `      "attachment": ${m.attachment},\n`
      jsonc += `      // Bật cờ này nếu model hỗ trợ tính năng suy luận (reasoning/deep thinking)\n`
      jsonc += `      "reasoning": ${m.reasoning},\n`
      jsonc += `      // Bật cờ này để cho phép model thực hiện gọi hàm (function calling/tool call)\n`
      jsonc += `      "tool_call": ${m.tool_call},\n`
      jsonc += `      // Giới hạn Token đầu vào và đầu ra cho model (Bắt buộc trong OpenCode custom model)\n`
      jsonc += `      "limit": {\n`
      jsonc += `        "context": ${m.maxInputTokens},\n`
      jsonc += `        "output": ${m.maxOutputTokens}\n`
      jsonc += `      }\n`
      jsonc += `    }${isLast ? '' : ','}\n`
    })

    jsonc += `  }\n`
    jsonc += `}\n`

    return jsonc
  }

  const enabledCount = modelsList.filter(m => m.enabled).length

  return (
    <div className="animate-fade-in space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-semibold text-white">OpenCode Config Generator</h2>
          <p className="text-sm text-slate-400">Tự động tạo tệp cấu hình phù hợp với OpenCode client kết nối qua proxy của bạn.</p>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Left Side: Controls */}
        <div className="space-y-6">
          {/* General Provider Options */}
          <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
            <h3 className="text-sm font-semibold text-indigo-400 uppercase tracking-wider">Cấu hình chung</h3>
            
            <div className="grid gap-4 md:grid-cols-2">
              <div>
                <label className="text-xs text-slate-400 block mb-1.5 font-medium">Provider ID</label>
                <input 
                  type="text" 
                  value={providerId} 
                  onChange={(e) => setProviderId(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, ''))} 
                  className="input w-full"
                  placeholder="e.g. kiro"
                />
              </div>

              <div>
                <label className="text-xs text-slate-400 block mb-1.5 font-medium">API Format</label>
                <select 
                  value={apiFormat} 
                  onChange={(e) => setApiFormat(e.target.value)} 
                  className="input w-full bg-slate-800 border-slate-700 text-slate-200"
                >
                  <option value="openai">OpenAI (v1/chat/completions)</option>
                  <option value="anthropic">Anthropic (v1/messages)</option>
                </select>
              </div>

              <div className="md:col-span-2">
                <label className="text-xs text-slate-400 block mb-1.5 font-medium">Base URL</label>
                <input 
                  type="text" 
                  value={baseURL} 
                  onChange={(e) => setBaseURL(e.target.value)} 
                  className="input w-full font-mono text-xs"
                  placeholder="http://localhost:8765/v1"
                />
              </div>

              <div className="md:col-span-2">
                <label className="text-xs text-slate-400 block mb-1.5 font-medium">API Key (Proxy API Key)</label>
                <input 
                  type="text" 
                  value={apiKey} 
                  onChange={(e) => setApiKey(e.target.value)} 
                  className="input w-full font-mono text-xs"
                  placeholder="Nhập proxy_api_key của bạn"
                />
              </div>
            </div>
          </section>

          {/* Models Selector */}
          <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold text-indigo-400 uppercase tracking-wider">Cấu hình các Models ({enabledCount}/{modelsList.length})</h3>
              {modelsList.length > 0 && (
                <div className="flex gap-2">
                  <button 
                    onClick={() => setModelsList(prev => prev.map(m => ({ ...m, enabled: true })))}
                    className="text-xs text-indigo-400 hover:text-indigo-300 font-medium transition-colors"
                  >
                    Chọn tất cả
                  </button>
                  <span className="text-slate-600 text-xs">|</span>
                  <button 
                    onClick={() => setModelsList(prev => prev.map(m => ({ ...m, enabled: false })))}
                    className="text-xs text-slate-500 hover:text-slate-400 font-medium transition-colors"
                  >
                    Bỏ chọn tất cả
                  </button>
                </div>
              )}
            </div>

            {modelsList.length === 0 ? (
              <div className="py-8 text-center text-slate-500 text-sm">
                Đang tải danh sách models từ proxy...
              </div>
            ) : (
              <div className="space-y-4 max-h-[420px] overflow-y-auto pr-1">
                {modelsList.map((m) => (
                  <div 
                    key={m.id} 
                    className={`p-3 rounded-lg border transition-all ${
                      m.enabled 
                        ? 'bg-slate-800/40 border-slate-700/80' 
                        : 'bg-slate-950/20 border-slate-900/60 opacity-60'
                    }`}
                  >
                    <div className="flex items-start justify-between gap-3 mb-2.5">
                      <div className="flex items-center gap-3">
                        <input 
                          type="checkbox" 
                          checked={m.enabled} 
                          onChange={() => handleToggleModel(m.id)}
                          className="h-4.5 w-4.5 rounded border-slate-700 bg-slate-800 text-indigo-600 focus:ring-indigo-500/30"
                        />
                        <div>
                          <div className="text-sm font-semibold text-slate-200">{m.name}</div>
                          <div className="text-xs font-mono text-slate-500">{m.id}</div>
                        </div>
                      </div>
                    </div>

                    {m.enabled && (
                      <div className="grid grid-cols-3 gap-2 pt-2 border-t border-slate-800/60 mt-1">
                        <div className="flex flex-col justify-end">
                          <label className="flex items-center gap-1.5 cursor-pointer py-1">
                            <input 
                              type="checkbox" 
                              checked={m.reasoning} 
                              onChange={(e) => handleModelPropChange(m.id, 'reasoning', e.target.checked)}
                              className="h-3.5 w-3.5 rounded border-slate-700 bg-slate-800 text-indigo-600"
                            />
                            <span className="text-[11px] text-slate-400">Reasoning</span>
                          </label>
                        </div>
                        <div className="flex flex-col justify-end">
                          <label className="flex items-center gap-1.5 cursor-pointer py-1">
                            <input 
                              type="checkbox" 
                              checked={m.tool_call} 
                              onChange={(e) => handleModelPropChange(m.id, 'tool_call', e.target.checked)}
                              className="h-3.5 w-3.5 rounded border-slate-700 bg-slate-800 text-indigo-600"
                            />
                            <span className="text-[11px] text-slate-400">Tool Call</span>
                          </label>
                        </div>
                        <div className="flex flex-col justify-end">
                          <label className="flex items-center gap-1.5 cursor-pointer py-1">
                            <input 
                              type="checkbox" 
                              checked={m.attachment} 
                              onChange={(e) => handleModelPropChange(m.id, 'attachment', e.target.checked)}
                              className="h-3.5 w-3.5 rounded border-slate-700 bg-slate-800 text-indigo-600"
                            />
                            <span className="text-[11px] text-slate-400">Attachment</span>
                          </label>
                        </div>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>

        {/* Right Side: Live Code Preview */}
        <div className="space-y-6 flex flex-col h-full">
          <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 flex-1 flex flex-col min-h-[450px]">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-4">
              <div>
                <h3 className="text-sm font-semibold text-indigo-400 uppercase tracking-wider">Xem trước cấu hình</h3>
                <span className="text-xs text-slate-500 font-mono">Opencode.jsonc</span>
              </div>

              {/* Default Model Quick Copy Helper */}
              <div className="flex items-center gap-3">
                {defaultModelId && (
                  <div className="hidden sm:flex items-center gap-2 bg-slate-950 px-2.5 py-1 rounded-lg border border-slate-800 text-xs">
                    <span className="text-slate-400">Model gợi ý:</span>
                    <code 
                      className="text-indigo-400 font-mono select-all cursor-pointer hover:text-indigo-300 transition-colors"
                      title="Click để sao chép model ID"
                      onClick={() => {
                        navigator.clipboard.writeText(`${providerId}/${defaultModelId}`)
                        toast(`Đã sao chép model ID: ${providerId}/${defaultModelId}`, 'success')
                      }}
                    >
                      {providerId}/{defaultModelId}
                    </code>
                  </div>
                )}

                <button 
                  onClick={handleCopy}
                  className={`flex items-center gap-2 px-3 py-1 text-xs font-semibold rounded-lg transition-all ${
                    copied 
                      ? 'bg-emerald-600/20 text-emerald-400 border border-emerald-500/30' 
                      : 'bg-indigo-600 hover:bg-indigo-500 text-white'
                  }`}
                >
                  {copied ? (
                    <>
                      <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2">
                        <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                      </svg>
                      Copied!
                    </>
                  ) : (
                    <>
                      <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2">
                        <path strokeLinecap="round" strokeLinejoin="round" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3" />
                      </svg>
                      Copy Config
                    </>
                  )}
                </button>
              </div>
            </div>

            <div className="relative flex-1 bg-slate-950/80 rounded-lg border border-slate-800 p-4 font-mono text-xs overflow-auto max-h-[500px]">
              <pre className="text-slate-300 leading-relaxed whitespace-pre-wrap">{generateJsoncString()}</pre>
            </div>

            <div className="mt-4 pt-4 border-t border-slate-800/80 space-y-2">
              <h4 className="text-xs font-semibold text-slate-300">💡 Hướng dẫn cài đặt cấu hình cho OpenCode:</h4>
              <p className="text-xs text-slate-400 leading-relaxed">
                1. Sao chép khối cấu hình của nhà cung cấp (provider) ở trên.<br />
                2. Mở file cấu hình sẵn có của bạn (<code className="text-indigo-400 font-mono">config.json</code>) tại:<br />
                &nbsp;&nbsp;&nbsp;&bull;&nbsp;<strong>macOS / Linux:</strong> <code className="text-indigo-400 font-mono">~/.config/opencode/config.json</code><br />
                &nbsp;&nbsp;&nbsp;&bull;&nbsp;<strong>Windows:</strong> <code className="text-indigo-400 font-mono">%APPDATA%\opencode\config.json</code><br />
                3. Tìm khóa <code className="text-indigo-400 font-mono">"provider": &#123; ... &#125;</code> và dán khối mã vừa sao chép vào bên trong (chú ý thêm dấu phẩy ngăn cách giữa các provider).<br />
                4. Cập nhật khóa <code className="text-indigo-400 font-mono">"model"</code> ở cấp cao nhất thành model bạn muốn sử dụng (ví dụ: click vào <strong className="text-indigo-400 cursor-pointer" onClick={() => {
                  if (defaultModelId) {
                    navigator.clipboard.writeText(`${providerId}/${defaultModelId}`);
                    toast(`Đã sao chép model ID: ${providerId}/${defaultModelId}`, 'success');
                  }
                }}>{providerId}/{defaultModelId}</strong> ở góc trên để copy nhanh và paste).<br />
                5. Lưu file và khởi động lại OpenCode.
              </p>
            </div>
          </section>
        </div>
      </div>
    </div>
  )
}
