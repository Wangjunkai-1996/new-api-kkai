local BODY_SCAN_LIMIT = 2 * 1024 * 1024
local RULES_VERSION = "2026-05-18.role-aware.v3"
local DEFAULT_RULES_PATH = "/opt/ai-risk-guard/rules/pre-risk-rules.json"
local DEFAULT_EVENT_LOG_PATH = "/var/lib/ai-risk-guard/events.jsonl"
local DEFAULT_BLOCK_TTL_SECONDS = 900
local STRIKE_TTL_SECONDS = 24 * 60 * 60
local STRIKE_THRESHOLD = 2
local RULE_RELOAD_SECONDS = 10
local STATE_KEY = "__ai_risk_guard_state_v3"

local ok_cjson, cjson = pcall(require, "cjson.safe")

local BUILTIN_RULES = {
  version = RULES_VERSION .. ".builtin",
  event_log_path = DEFAULT_EVENT_LOG_PATH,
  block_ttl_seconds = DEFAULT_BLOCK_TTL_SECONDS,
  allow_key_fingerprints = {
    "762a7a3ba2088f8b74c491d8f572c8f9",
    "7b747fd33045cb3b78087e306fac1d1c",
  },
  rules = {
    {
      case_id = "pwn_tcache_free_hook_chain",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"pwn", "heap", "tcache", "free-hook"},
      description = "High-confidence heap exploitation chain combining tcache poisoning and __free_hook overwrite intent.",
      all = {
        [[(?i)\btcache(?:[-_\s]*poison(?:ing)?)?\b]],
        [[__free_hook]],
      },
      any = {
        [[(?i)\b(?:malloc|free|glibc|heap|one_gadget|system\s*\(|overwrite|exploit|pwn)\b]],
        [[覆盖|劫持|写入|getshell]],
      },
    },
    {
      case_id = "pwn_rop_flag_open_read_write_chain",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"pwn", "rop", "flag-read", "exfiltration"},
      description = "High-confidence ROP chain to open ./flag, read it, and write it out.",
      all = {
        [[(?i)\bROP\b|return[-\s]?oriented]],
        [=[(?i)\bopen(?:at)?\s*\([^)]*["'](?:\./|/)?flag(?:\.txt)?["']]=],
        [[(?i)\bread\s*\(]],
        [[(?i)\bwrite\s*\(]],
      },
      any = {
        [[(?i)\b(?:syscall|gadget|pop\s+rdi|ret2csu|ret2libc|payload|exploit)\b]],
        [[(?i)\b(?:stdout|fd|sendfile|leak)\b]],
      },
    },
    {
      case_id = "pwn_flag_file_open_read_write_chain",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"pwn", "flag-read", "openat", "exfiltration"},
      description = "High-confidence exploit payload chain to open/openat a flag path, read it, and write it out.",
      all = {
        [[(?i)\b(?:pwn|exploit|payload|shellcode|ROP|return[-\s]?oriented)\b]],
        [=[(?i)\bopen(?:at)?\s*\([^)]*["'](?:\./|/)?flag(?:\.txt)?["']]=],
        [[(?i)\bread\s*\(]],
        [[(?i)\bwrite\s*\(]],
      },
      any = {
        [[(?i)\b(?:syscall|gadget|stdout|fd|sendfile|leak|AT_FDCWD|O_RDONLY)\b]],
      },
    },
    {
      case_id = "pwn_tcache_malloc_hook_chain",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"pwn", "heap", "tcache", "malloc-hook"},
      description = "High-confidence heap exploitation chain combining tcache poisoning and __malloc_hook overwrite intent.",
      all = {
        [[(?i)\btcache(?:[-_\s]*poison(?:ing)?)?\b]],
        [[__malloc_hook]],
      },
      any = {
        [[(?i)\b(?:malloc|free|glibc|heap|one_gadget|system\s*\(|overwrite|exploit|pwn)\b]],
        [[覆盖|劫持|写入|getshell]],
      },
    },
    {
      case_id = "game_reverse_named_tooling_action_chain",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"game-reverse", "named-tooling", "hook", "dump"},
      description = "High-confidence game reverse chain combining named tooling/runtime markers with hook, dump, bypass, anti-screenshot, watermark, login, or role-creation actions.",
      all = {
        [[(?i)\b(?:AnantaCracker(?:\.dll)?|DumpedLua|tolua(?:\.dll)?|xlua(?:\.dll)?|luaL_loadbuffer|frida|xposed|zygisk)\b]],
        [[(?i)\b(?:hook|dump|bypass)\b|绕过|反截图|水印|登录|创角]],
      },
      any = {
        [[(?i)\b(?:game|client|launcher|apk|mobile|unity|lua|ananta)\b|游戏|客户端|手游|账号|角色|登录|创角|反截图|水印]],
      },
    },
    {
      case_id = "ananta_cracker_dumped_lua_chain",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"game-reverse", "lua-dump", "hook", "sample-match"},
      description = "Exact high-confidence AnantaCracker/DumpedLua/Lua hook reverse-engineering chain.",
      all = {
        [[(?i)(?:\bAnantaCracker(?:\.dll)?\b|\bDumpedLua\b|DumpedLua)|Lua\s*反编译]],
        [[(?i)(?:(?:tolua|xlua)\.dll|luaL_loadbuffer|LuaDLL\.luaL_loadbuffer|hook|dump)|拦截|导出|反编译]],
      },
      any = {
        [[去除?|移除|关闭|禁用|绕过]],
        [[反截图|防截图|截屏保护|水印]],
        [[(?i)\b(?:CheckAccount|TryLogin|RequestEnterGame|RPC|TCP)\b|创角|已有角色]],
      },
    },
    {
      case_id = "lua_runtime_hook_dump_reverse_chain",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"game-reverse", "lua", "hook", "dump"},
      description = "Lua runtime hook/dump/decompile request against tolua/xlua/native loading functions.",
      all = {
        [[(?i)(?:luaL_loadbuffer|LuaDLL\.luaL_loadbuffer|tolua\.dll|xlua\.dll)]],
        [[(?i)(?:hook|detour|inline\s*hook|dump|DumpedLua)|反编译|解密|加密|导出|拦截|内存]],
      },
      any = {
        [[游戏|客户端|launcher|native|dll|Ananta|Unity|Lua]],
        [[打印.{0,20}调用点|去水印|反截图|绕过]],
      },
    },
    {
      case_id = "game_account_rpc_flow_bypass",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"game-reverse", "account-flow-bypass", "rpc"},
      description = "Requests to bypass or forge game account/RPC/role creation flows.",
      all = {
        [[(?i)\b(?:CheckAccount|TryLogin|RequestEnterGame)\b|(?:绕过|伪造|假装|跳过).{0,40}(?:创角|创建角色|已有角色|创角\s*UI)|(?:创角|创建角色|已有角色|创角\s*UI).{0,40}(?:绕过|伪造|假装|跳过)]],
        [[绕过|伪造|假装|跳过|打印.{0,20}调用点|hook|拦截|抓包]],
      },
      any = {
        [[已有角色|角色已存在|创角|创建角色|创角\s*UI|客户端|游戏]],
      },
    },
    {
      case_id = "screenshot_or_watermark_protection_bypass_cn",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"game-reverse", "watermark-bypass", "screenshot-bypass"},
      description = "Chinese requests to remove watermarking or anti-screenshot protections in a client/game context.",
      all = {
        [[(?:去除?|移除|关闭|禁用|绕过).{0,30}(?:水印|反截图|防截图|截屏保护)|(?:水印|反截图|防截图|截屏保护).{0,30}(?:去除?|移除|关闭|禁用|绕过)]],
        [[游戏|客户端|dll|native|hook|内存|Ananta|Lua|(?i:\bRPC\b)]],
      },
      any = {
        [[dump|DumpedLua|反编译|导出|拦截|破解|hook|绕过]],
      },
    },
    {
      case_id = "english_game_client_protection_bypass_chain",
      enabled = true,
      severity = "critical",
      enforcement = "block",
      tags = {"game-reverse", "protection-bypass", "english"},
      description = "High-confidence English game/client protection bypass request with game context, bypass action, protected surface, and tooling/runtime signal.",
      all = {
        [[(?i)\b(?:game|client|apk|mobile|unity|unreal|android|ios)\b]],
        [[(?i)\b(?:bypass(?:es|ing)?|disable[sd]?|remov(?:e|es|ing)|strip(?:s|ping)?|evad(?:e|es|ing)|spoof(?:s|ing)?|skip(?:s|ping)?)\b]],
        [[(?i)\b(?:anti[- ]?cheat|watermark|anti[- ]?screenshot|screenshot protection|root detection|emulator detection|integrity check|login check|account check|role creation)\b]],
      },
      any = {
        [[(?i)\b(?:frida|xposed|zygisk|dobby|minhook|substrate|hook|inline hook|patch|dump|luaL_loadbuffer|tolua|xlua|il2cpp|GameAssembly|libil2cpp)\b]],
        [[(?i)\b(?:script|module|runtime|binary|offset|function)\b]],
      },
    },
    {
      case_id = "generic_game_reverse_compound_strike",
      enabled = true,
      severity = "medium",
      enforcement = "observe",
      tags = {"game-reverse", "compound-signal", "strike"},
      description = "Medium-confidence scored signal for review only; it never blocks or bans by itself.",
      score_threshold = 6,
      signals = {
        {id = "game_or_client_context", score = 2, pattern = [[游戏|客户端|launcher|Unity|Unreal|apk|手游|网游]]},
        {id = "binary_or_runtime_context", score = 2, pattern = [[(?i)\b(?:dll|native|il2cpp|GameAssembly|libil2cpp|tolua|xlua|luaL_loadbuffer|frida|xposed|zygisk)\b|内存]]},
        {id = "reverse_action", score = 3, pattern = [[(?i)\b(?:crack|cracker|hook|dump|bypass|detour|patch)\b|破解|反编译|绕过|导出|拦截]]},
        {id = "account_flow_or_protection", score = 2, pattern = [[(?i)\b(?:CheckAccount|TryLogin|RequestEnterGame|RPC|TCP)\b|创角|账号|登录|水印|反截图|防截图]]},
      },
    },
    {
      case_id = "generic_single_security_term_observe",
      enabled = true,
      severity = "low",
      enforcement = "observe",
      tags = {"generic-term", "observe-only"},
      description = "Single generic technical terms are evidence-only and must not auto-block or auto-ban without a stronger chain rule.",
      pattern = [[(?i)\b(?:tcache|ROP|__free_hook|__malloc_hook|openat|flag\.txt|frida|xposed|zygisk|luaL_loadbuffer|tolua|xlua|hook|dump|bypass|RPC)\b|反截图]],
    },
  },
}

local json_escape = function(value)
  value = tostring(value)
  value = string.gsub(value, "\\", "\\\\")
  value = string.gsub(value, "\"", "\\\"")
  value = string.gsub(value, "\n", "\\n")
  value = string.gsub(value, "\r", "\\r")
  value = string.gsub(value, "\t", "\\t")
  return "\"" .. value .. "\""
end

local is_array = function(value)
  if type(value) ~= "table" then
    return false
  end
  local max = 0
  local count = 0
  for key, _ in pairs(value) do
    if type(key) ~= "number" then
      return false
    end
    if key > max then
      max = key
    end
    count = count + 1
  end
  return max == count
end

local json_encode
json_encode = function(value)
  local kind = type(value)
  if kind == "nil" then
    return "null"
  end
  if kind == "boolean" or kind == "number" then
    return tostring(value)
  end
  if kind == "string" then
    return json_escape(value)
  end
  if kind ~= "table" then
    return json_escape(value)
  end

  local parts = {}
  if is_array(value) then
    for index = 1, #value do
      parts[#parts + 1] = json_encode(value[index])
    end
    return "[" .. table.concat(parts, ",") .. "]"
  end

  for key, item in pairs(value) do
    if item ~= nil then
      parts[#parts + 1] = json_escape(key) .. ":" .. json_encode(item)
    end
  end
  return "{" .. table.concat(parts, ",") .. "}"
end

local state = rawget(_G, STATE_KEY)
if not state then
  state = {
    loaded_at = 0,
    rules = nil,
    rules_path = nil,
    event_log_path = DEFAULT_EVENT_LOG_PATH,
    block_ttl_seconds = DEFAULT_BLOCK_TTL_SECONDS,
  }
  rawset(_G, STATE_KEY, state)
end

local now = function()
  return ngx.now()
end

local log_error = function(message)
  ngx.log(ngx.ERR, "[ai-risk-guard] ", message)
end

local response_json = function(status, payload)
  ngx.status = status
  ngx.header["Content-Type"] = "application/json; charset=utf-8"
  ngx.header["X-Risk-Case-Id"] = payload.risk_case_id

  ngx.say(ok_cjson and cjson and cjson.encode(payload) or json_encode(payload))

  return ngx.exit(status)
end

local is_text_body = function(content_type)
  if not content_type or content_type == "" then
    return true
  end

  local normalized = string.lower(content_type)
  if string.find(normalized, "multipart/form-data", 1, true) then
    return true
  end
  if string.find(normalized, "application/json", 1, true) then
    return true
  end
  if string.find(normalized, "+json", 1, true) then
    return true
  end
  if string.find(normalized, "application/x%-ndjson") then
    return true
  end
  if string.find(normalized, "text/", 1, true) then
    return true
  end

  return false
end

local read_file_prefix = function(path, limit)
  local file, open_err = io.open(path, "rb")
  if not file then
    return nil, open_err
  end

  local data = file:read(limit)
  file:close()
  return data or "", nil
end

local read_body_prefix = function()
  local content_type = ngx.var.content_type
  if not is_text_body(content_type) then
    return "", false, nil
  end

  ngx.req.read_body()

  local body = ngx.req.get_body_data()
  if body then
    return string.sub(body, 1, BODY_SCAN_LIMIT), #body > BODY_SCAN_LIMIT, nil
  end

  local body_file = ngx.req.get_body_file()
  if not body_file then
    return "", false, nil
  end

  local data, err = read_file_prefix(body_file, BODY_SCAN_LIMIT)
  if not data then
    return "", false, err
  end

  local truncated = false
  local file_size = tonumber(ngx.var.request_length)
  if file_size and file_size > BODY_SCAN_LIMIT then
    truncated = true
  end

  return data, truncated, nil
end

local is_json_body = function(content_type)
  if not content_type or content_type == "" then
    return true
  end

  local normalized = string.lower(content_type)
  return string.find(normalized, "application/json", 1, true) ~= nil
      or string.find(normalized, "+json", 1, true) ~= nil
      or string.find(normalized, "application/x%-ndjson") ~= nil
end

local is_multipart_body = function(content_type)
  if not content_type or content_type == "" then
    return false
  end
  return string.find(string.lower(content_type), "multipart/form-data", 1, true) ~= nil
end

local endpoint_kind = function()
  local uri = ngx.var.uri or ""
  if uri == "/v1/responses" or uri == "/v1beta/responses" then
    return "responses"
  end
  if uri == "/v1/chat/completions" or uri == "/v1beta/chat/completions" then
    return "chat_completions"
  end
  if uri == "/v1/completions" or uri == "/v1beta/completions" then
    return "completions"
  end
  return nil
end

local append_text = function(parts, value)
  if type(value) ~= "string" or value == "" then
    return
  end
  parts[#parts + 1] = value
end

local normalize_token = function(value)
  if type(value) ~= "string" then
    return ""
  end
  return string.lower(value)
end

local content_part_has_user_text = function(part)
  if type(part) ~= "table" then
    return false
  end

  local part_type = normalize_token(part.type)
  return part_type == "" or part_type == "text" or part_type == "input_text"
end

local append_content_text = function(parts, content)
  if type(content) == "string" then
    append_text(parts, content)
    return
  end
  if type(content) ~= "table" then
    return
  end

  if is_array(content) then
    for _, part in ipairs(content) do
      if type(part) == "string" then
        append_text(parts, part)
      elseif content_part_has_user_text(part) then
        append_text(parts, part.text)
        append_text(parts, part.input_text)
      end
    end
    return
  end

  if content_part_has_user_text(content) then
    append_text(parts, content.text)
    append_text(parts, content.input_text)
  end
end

local append_responses_input_item = function(parts, item)
  if type(item) == "string" then
    append_text(parts, item)
    return
  end
  if type(item) ~= "table" then
    return
  end

  local item_type = normalize_token(item.type)
  local role = normalize_token(item.role)
  if item_type == "input_text" and (role == "" or role == "user") then
    append_text(parts, item.text)
    append_text(parts, item.input_text)
    return
  end

  if role ~= "user" then
    return
  end

  append_content_text(parts, item.content)
  if item_type == "text" or item_type == "message" or item_type == "" then
    append_text(parts, item.text)
    append_text(parts, item.input_text)
  end
end

local is_responses_user_authored_item = function(item)
  if type(item) == "string" then
    return true
  end
  if type(item) ~= "table" then
    return false
  end

  local item_type = normalize_token(item.type)
  local role = normalize_token(item.role)
  if item_type == "input_text" then
    return role == "" or role == "user"
  end

  return role == "user"
end

local extract_responses_scan_text = function(decoded)
  local parts = {}
  local input = decoded.input

  if type(input) == "table" and is_array(input) then
    for index = #input, 1, -1 do
      local item = input[index]
      if is_responses_user_authored_item(item) then
        append_responses_input_item(parts, item)
        return table.concat(parts, "\n"), {
          scan_scope = "responses_final_user_input",
          scan_item_index = index,
        }
      end
    end
    return "", {scan_scope = "responses_final_user_input", scan_item_index = nil}
  else
    append_responses_input_item(parts, input)
  end

  return table.concat(parts, "\n"), {scan_scope = "responses_direct_input"}
end

local extract_chat_scan_text = function(decoded)
  local parts = {}
  if type(decoded.messages) ~= "table" then
    return "", {scan_scope = "chat_final_user_message"}
  end

  for index = #decoded.messages, 1, -1 do
    local message = decoded.messages[index]
    if type(message) == "table" and normalize_token(message.role) == "user" then
      append_content_text(parts, message.content)
      return table.concat(parts, "\n"), {
        scan_scope = "chat_final_user_message",
        scan_message_index = index,
      }
    end
  end

  return "", {scan_scope = "chat_final_user_message", scan_message_index = nil}
end

local append_prompt_text = function(parts, prompt)
  if type(prompt) == "string" then
    append_text(parts, prompt)
    return
  end
  if type(prompt) ~= "table" or not is_array(prompt) then
    return
  end

  for _, item in ipairs(prompt) do
    append_prompt_text(parts, item)
  end
end

local extract_completion_scan_text = function(decoded)
  local parts = {}
  append_prompt_text(parts, decoded.prompt)
  return table.concat(parts, "\n"), {scan_scope = "completions_prompt"}
end

local multipart_boundary = function(content_type)
  if not content_type then
    return nil
  end

  local match = ngx.re.match(content_type, [[boundary=(?:"([^"]+)"|([^;\s]+))]], "ijo")
  if not match then
    return nil
  end
  return match[1] or match[2]
end

local multipart_part_value = function(part)
  part = string.gsub(part, "^\r?\n", "")

  local header_from, header_to = string.find(part, "\r\n\r\n", 1, true)
  if not header_from then
    header_from, header_to = string.find(part, "\n\n", 1, true)
  end
  if not header_from then
    return nil
  end

  local headers = string.sub(part, 1, header_from - 1)
  local value = string.sub(part, header_to + 1)
  value = string.gsub(value, "\r?\n$", "")

  local normalized = string.lower(headers)
  if not string.find(normalized, "content%-disposition:%s*form%-data") then
    return nil
  end
  if string.find(normalized, "filename%s*=") then
    return nil
  end

  local part_content_type = string.match(normalized, "\ncontent%-type:%s*([^;\r\n]+)")
      or string.match(normalized, "^content%-type:%s*([^;\r\n]+)")
  if part_content_type
      and string.sub(part_content_type, 1, 5) ~= "text/"
      and not string.find(part_content_type, "json", 1, true)
      and not string.find(part_content_type, "x-www-form-urlencoded", 1, true) then
    return nil
  end

  return value
end

local extract_multipart_scan_text = function(body, content_type)
  local boundary = multipart_boundary(content_type)
  if not boundary or boundary == "" then
    return "", {scan_scope = "multipart_text_fields", scan_text_fields = 0}
  end

  local delimiter = "--" .. boundary
  local parts = {}
  local cursor = 1

  while true do
    local part_start, part_start_end = string.find(body, delimiter, cursor, true)
    if not part_start then
      break
    end

    local next_start = string.find(body, delimiter, part_start_end + 1, true)
    if not next_start then
      break
    end

    local value = multipart_part_value(string.sub(body, part_start_end + 1, next_start - 1))
    append_text(parts, value)
    cursor = next_start
  end

  return table.concat(parts, "\n"), {
    scan_scope = "multipart_text_fields",
    scan_text_fields = #parts,
  }
end

local extract_json_scan_text = function(body, content_type)
  local kind = endpoint_kind()
  if not ok_cjson or not cjson or not is_json_body(content_type) then
    if kind then
      return "", {scan_scope = kind .. "_parse_unavailable"}
    end
    return body, {scan_scope = "raw_body"}
  end

  local decoded = cjson.decode(body)
  if type(decoded) ~= "table" then
    if kind then
      return "", {scan_scope = kind .. "_parse_failed"}
    end
    return body, {scan_scope = "raw_body"}
  end

  if kind == "responses" then
    return extract_responses_scan_text(decoded)
  end
  if kind == "chat_completions" then
    return extract_chat_scan_text(decoded)
  end
  if kind == "completions" then
    return extract_completion_scan_text(decoded)
  end

  return body, {scan_scope = "json_raw_body"}
end

local scan_text_from_body = function(body, content_type)
  if not body or body == "" then
    return "", {scan_scope = "empty"}
  end
  if is_multipart_body(content_type) then
    return extract_multipart_scan_text(body, content_type)
  end
  return extract_json_scan_text(body, content_type)
end

local load_rules = function()
  local rules_path = DEFAULT_RULES_PATH
  local current_time = now()

  if state.rules and state.rules_path == rules_path and current_time - state.loaded_at < RULE_RELOAD_SECONDS then
    return state.rules, nil
  end

  if not ok_cjson or not cjson then
    state.rules = BUILTIN_RULES
    state.rules_path = rules_path
    state.loaded_at = current_time
    state.event_log_path = BUILTIN_RULES.event_log_path or DEFAULT_EVENT_LOG_PATH
    state.block_ttl_seconds = tonumber(BUILTIN_RULES.block_ttl_seconds) or DEFAULT_BLOCK_TTL_SECONDS
    return BUILTIN_RULES, nil
  end

  local raw_rules, read_err = read_file_prefix(rules_path, 10 * 1024 * 1024)
  if not raw_rules then
    return nil, "unable to read rules file " .. rules_path .. ": " .. tostring(read_err)
  end

  local decoded, decode_err = cjson.decode(raw_rules)
  if not decoded then
    return nil, "unable to decode rules file " .. rules_path .. ": " .. tostring(decode_err)
  end

  state.rules = decoded
  state.rules_path = rules_path
  state.loaded_at = current_time
  state.event_log_path = decoded.event_log_path or DEFAULT_EVENT_LOG_PATH
  state.block_ttl_seconds = tonumber(decoded.block_ttl_seconds) or DEFAULT_BLOCK_TTL_SECONDS

  return decoded, nil
end

local header_value = function(name)
  local value = ngx.req.get_headers()[name]
  if type(value) == "table" then
    value = value[1]
  end
  return value
end

local credential_fingerprint = function()
  local authorization = header_value("authorization") or header_value("Authorization")
  local api_key = header_value("x-api-key") or header_value("X-API-Key")
      or header_value("api-key") or header_value("Api-Key")

  local raw = authorization or api_key
  if not raw or raw == "" then
    return nil, nil, nil
  end

  local token = raw
  token = string.gsub(token, "^[Bb]earer%s+", "")
  local normalized = token
  normalized = string.gsub(normalized, "^sk%-", "")
  local suffix = string.sub(token, -8)

  return ngx.md5(normalized), token, suffix
end

local dict_get = function(key)
  local dict = ngx.shared.ai_risk_guard_v8
  if not dict then
    return nil
  end
  return dict:get(key)
end

local dict_set = function(key, value, ttl)
  local dict = ngx.shared.ai_risk_guard_v8
  if not dict then
    return
  end
  local ok, err = dict:set(key, value, ttl)
  if not ok then
    log_error("shared dict set failed for " .. key .. ": " .. tostring(err))
  end
end

local list_contains = function(values, needle)
  if not needle or type(values) ~= "table" then
    return false
  end

  for _, value in ipairs(values) do
    if tostring(value) == needle then
      return true
    end
  end

  return false
end

local is_key_allowlisted = function(rules_doc, key_fp)
  if not key_fp then
    return false
  end

  return list_contains(rules_doc and rules_doc.allow_key_fingerprints, key_fp)
      or list_contains(rules_doc and rules_doc.allow_key_hashes, key_fp)
end

local find_pattern = function(text, pattern)
  if not pattern or pattern == "" then
    return nil
  end

  local from, to, err = ngx.re.find(text, pattern, "ijo")
  if err then
    log_error("invalid rule pattern: " .. tostring(err))
    return nil
  end

  if from then
    return from, to
  end
  return nil
end

local match_pattern = function(text, pattern)
  local from = find_pattern(text, pattern)
  return from ~= nil
end

local matches_all = function(text, patterns)
  if type(patterns) ~= "table" then
    return true
  end

  for _, pattern in ipairs(patterns) do
    if not match_pattern(text, pattern) then
      return false
    end
  end

  return true
end

local matches_any = function(text, patterns)
  if type(patterns) ~= "table" or #patterns == 0 then
    return true
  end

  for _, pattern in ipairs(patterns) do
    if match_pattern(text, pattern) then
      return true
    end
  end

  return false
end

local score_rule = function(text, rule)
  local threshold = tonumber(rule.score_threshold)
  if not threshold or type(rule.signals) ~= "table" then
    return false, nil, nil, nil, nil, nil
  end

  local total = 0
  local matched = {}
  local first_pattern
  local first_from
  local first_to

  for _, signal in ipairs(rule.signals) do
    local pattern = signal.pattern
    local from, to = find_pattern(text, pattern)
    if from then
      local score = tonumber(signal.score) or 1
      total = total + score
      matched[#matched + 1] = {
        id = signal.id,
        score = score,
        pattern = pattern,
      }
      if not first_pattern then
        first_pattern = pattern
        first_from = from
        first_to = to
      end
    end
  end

  return total >= threshold, first_pattern, first_from, first_to, matched, total
end

local find_rule_hit = function(text, rules_doc)
  local rules = rules_doc and rules_doc.rules
  if type(rules) ~= "table" then
    return nil
  end

  for _, rule in ipairs(rules) do
    local enabled = rule.enabled
    if enabled == nil or enabled == true then
      local matched = false
      local matched_pattern = rule.pattern
      local match_from, match_to
      local matched_signals, risk_score
      if rule.pattern then
        match_from, match_to = find_pattern(text, rule.pattern)
        matched = match_from ~= nil
      elseif rule.score_threshold then
        matched, matched_pattern, match_from, match_to, matched_signals, risk_score = score_rule(text, rule)
      else
        matched = matches_all(text, rule.all) and matches_any(text, rule.any)
        if matched and type(rule.all) == "table" then
          for _, pattern in ipairs(rule.all) do
            match_from, match_to = find_pattern(text, pattern)
            if match_from then
              matched_pattern = pattern
              break
            end
          end
        end
      end

      if matched then
        return rule, matched_pattern, match_from, match_to, matched_signals, risk_score
      end
    end
  end

  return nil
end

local excerpt = function(text, from, to)
  if not from then
    return string.sub(text, 1, 600)
  end
  local start_at = from - 220
  if start_at < 1 then
    start_at = 1
  end
  local end_at = (to or from) + 220
  if end_at > #text then
    end_at = #text
  end
  return string.sub(text, start_at, end_at)
end

local new_case_id = function(rule_id, remote_addr, key_fp)
  local seed = table.concat({
    rule_id or "rule",
    remote_addr or "unknown",
    key_fp or "no-key",
    tostring(ngx.now()),
    ngx.var.request_uri or "",
  }, "|")
  return "risk-" .. tostring(math.floor(ngx.now() * 1000)) .. "-" .. string.sub(ngx.md5(seed), 1, 12)
end

local model_from_body = function(body)
  if not body or body == "" then
    return nil
  end
  if not ok_cjson or not cjson then
    local match = ngx.re.match(body, [["model"\s*:\s*"([^"]+)"]], "jo")
    return match and match[1] or nil
  end
  local decoded = cjson.decode(body)
  if type(decoded) ~= "table" then
    return nil
  end
  if type(decoded.model) == "string" then
    return decoded.model
  end
  return nil
end

local should_reject_truncated = function()
  local uri = ngx.var.uri or ""
  return uri == "/v1/responses"
      or uri == "/v1/chat/completions"
      or uri == "/v1/completions"
      or uri == "/v1beta/responses"
      or uri == "/v1beta/chat/completions"
      or uri == "/v1beta/completions"
end

local redaction_marker = function(value)
  return "[REDACTED:" .. string.sub(ngx.md5(tostring(value or "")), 1, 12) .. "]"
end

local trim_string = function(value, limit)
  if type(value) ~= "string" or not limit or #value <= limit then
    return value, false
  end
  return string.sub(value, 1, limit) .. "...[truncated]", true
end

local sanitize_secret_text = function(value)
  if type(value) ~= "string" or value == "" then
    return value
  end

  local sanitized = string.gsub(value, "([Bb]earer%s+)([%w%._~%+%/%-]+=*)", function(prefix, token)
    if #token < 12 then
      return prefix .. token
    end
    return prefix .. redaction_marker(token)
  end)
  sanitized = string.gsub(sanitized, "(sk%-[%w%._%-]+)", function(token)
    if #token < 12 then
      return token
    end
    return redaction_marker(token)
  end)
  sanitized = string.gsub(sanitized, "([%w_%-%+/%=%.]+)", function(blob)
    if #blob < 48 then
      return blob
    end
    return redaction_marker(blob)
  end)

  return sanitized
end

local split_uri_query = function(uri)
  if type(uri) ~= "string" then
    return uri, nil
  end

  local query_at = string.find(uri, "?", 1, true)
  if not query_at then
    return uri, nil
  end

  return string.sub(uri, 1, query_at - 1), string.sub(uri, query_at + 1)
end

local sanitize_query_key = function(key)
  key = sanitize_secret_text(key or "")
  key, _ = trim_string(key, 120)
  return key
end

local query_key_is_sensitive = function(key)
  local normalized = string.lower(key or "")
  normalized = string.gsub(normalized, "[^a-z0-9]", "")
  return normalized == "apikey"
      or normalized == "key"
      or normalized == "token"
      or normalized == "authorization"
      or string.find(normalized, "apikey", 1, true) ~= nil
      or string.find(normalized, "secret", 1, true) ~= nil
      or string.find(normalized, "password", 1, true) ~= nil
      or string.find(normalized, "token", 1, true) ~= nil
end

local value_is_secret_like = function(value)
  if type(value) ~= "string" then
    return false
  end
  if string.find(value, "[Bb]earer%s+[%w%._~%+%/%-]+=*") then
    return true
  end
  if string.find(value, "sk%-[%w%._%-]+") then
    return true
  end
  return #value >= 48 and string.find(value, "^[%w_%-%+/%=%.]+$") ~= nil
end

local sanitize_query_metadata = function(query)
  if type(query) ~= "string" or query == "" then
    return nil
  end

  local metadata = {
    present = true,
    redacted = true,
    param_count = 0,
    param_names = {},
    redacted_params = {},
  }

  for pair in string.gmatch(query, "([^&;]+)") do
    local eq_at = string.find(pair, "=", 1, true)
    local key = eq_at and string.sub(pair, 1, eq_at - 1) or pair
    local value = eq_at and string.sub(pair, eq_at + 1) or ""
    local sanitized_key = sanitize_query_key(key)
    local sensitive = query_key_is_sensitive(key) or value_is_secret_like(value)

    metadata.param_count = metadata.param_count + 1
    metadata.param_names[#metadata.param_names + 1] = sanitized_key
    if sensitive then
      metadata.redacted_params[#metadata.redacted_params + 1] = {
        name = sanitized_key,
        value_hash = ngx.md5(value),
        value_length = #value,
      }
    end
  end

  return metadata
end

local sanitize_event_value
sanitize_event_value = function(value)
  local kind = type(value)
  if kind == "string" then
    return sanitize_secret_text(value)
  end
  if kind ~= "table" then
    return value
  end

  local sanitized = {}
  for key, item in pairs(value) do
    sanitized[key] = sanitize_event_value(item)
  end
  return sanitized
end

local sanitize_event = function(event)
  local sanitized = sanitize_event_value(event)
  if type(sanitized) ~= "table" then
    return sanitized
  end

  if type(event.uri) == "string" then
    local path, query = split_uri_query(event.uri)
    sanitized.uri = sanitize_secret_text(path)
    if query and query ~= "" then
      sanitized.query = sanitize_query_metadata(query)
    end
  end

  if type(event.matched_excerpt) == "string" then
    local raw_excerpt = event.matched_excerpt
    local limited, truncated = trim_string(raw_excerpt, 600)
    sanitized.matched_excerpt = sanitize_secret_text(limited)
    sanitized.matched_excerpt_hash = ngx.md5(raw_excerpt)
    sanitized.matched_excerpt_truncated = truncated
  end

  return sanitized
end

local write_event = function(event)
  event = sanitize_event(event)
  local encoded = ok_cjson and cjson and cjson.encode(event) or json_encode(event)
  if not encoded then
    return
  end

  local file, open_err = io.open(state.event_log_path, "a")
  if not file then
    log_error("event log open failed: " .. tostring(open_err))
    return
  end

  file:write(encoded, "\n")
  file:close()
end

local cached_block_decision = function(remote_addr, key_fp)
  local ip_case_id = dict_get("ip:" .. remote_addr)
  if ip_case_id then
    return {
      block = true,
      risk_case_id = ip_case_id,
      cached = true,
      subject = "ip",
    }
  end

  if key_fp then
    local key_case_id = dict_get("key:" .. key_fp)
    if key_case_id then
      return {
        block = true,
        risk_case_id = key_case_id,
        cached = true,
        subject = "key",
      }
    end
  end

  return nil
end

local write_oversize_evidence = function(remote_addr, key_fp, token_suffix, model, scan_text, scan_meta)
  local rule_id = "oversize_unreviewable_body"
  local risk_case_id = new_case_id(rule_id, remote_addr, key_fp)
  local scan_length = type(scan_text) == "string" and #scan_text or 0
  write_event({
    ts = ngx.utctime(),
    risk_case_id = risk_case_id,
    severity = "medium",
    action = "evidence_only",
    enforce = false,
    rule_id = rule_id,
    reason = "request body exceeded pre-risk scan limit without a high-confidence prefix hit",
    remote_addr = remote_addr,
    api_key_hash = key_fp,
    api_key_suffix = token_suffix,
    method = ngx.req.get_method(),
    uri = ngx.var.request_uri,
    host = ngx.var.host,
    xff_raw = header_value("x-forwarded-for") or header_value("X-Forwarded-For"),
    user_agent = header_value("user-agent") or header_value("User-Agent"),
    content_type = ngx.var.content_type,
    model = model,
    body_scan_limit_bytes = BODY_SCAN_LIMIT,
    body_truncated = true,
    scan_scope = scan_meta and scan_meta.scan_scope,
    scan_text_length = scan_length,
    scan_text_fields = scan_meta and scan_meta.scan_text_fields,
    matched_excerpt = scan_length > 0 and excerpt(scan_text) or nil,
    request_id = ngx.var.request_id,
  })
end

local evaluate = function()
  local remote_addr = ngx.var.remote_addr or "unknown"
  local key_fp, token, token_suffix = credential_fingerprint()

  local rules_doc, rules_err = load_rules()
  if not rules_doc then
    log_error(rules_err)
    return nil
  end

  if is_key_allowlisted(rules_doc, key_fp) then
    return nil
  end

  local cached = cached_block_decision(remote_addr, key_fp)
  if cached then
    write_event({
      ts = ngx.utctime(),
      risk_case_id = cached.risk_case_id,
      severity = "high",
      action = "block",
      cached = true,
      cache_subject = cached.subject,
      remote_addr = remote_addr,
      key_fingerprint = key_fp,
      method = ngx.req.get_method(),
      uri = ngx.var.request_uri,
      host = ngx.var.host,
      enforce = false,
      request_id = ngx.var.request_id,
    })
    return cached
  end

  local body, truncated, body_err = read_body_prefix()
  if body_err then
    log_error("body read failed: " .. tostring(body_err))
    return nil
  end
  if body == "" then
    return nil
  end

  local model = model_from_body(body)
  local scan_text, scan_meta = scan_text_from_body(body, ngx.var.content_type)
  if scan_text == "" and truncated and should_reject_truncated() then
    write_oversize_evidence(remote_addr, key_fp, token_suffix, model, scan_text, scan_meta)
    return nil
  end
  if scan_text == "" then
    return nil
  end

  local rule, matched_pattern, match_from, match_to, matched_signals, risk_score = find_rule_hit(scan_text, rules_doc)
  if not rule then
    if truncated and should_reject_truncated() then
      write_oversize_evidence(remote_addr, key_fp, token_suffix, model, scan_text, scan_meta)
      return nil
    end
    return nil
  end

  local rule_id = rule.case_id or "ai-risk-guard-rule-hit"
  local risk_case_id = new_case_id(rule_id, remote_addr, key_fp)
  local block_ttl = tonumber(rule.block_ttl_seconds) or state.block_ttl_seconds
  local enforcement = rule.enforcement or "block"
  local observe = enforcement == "observe" or enforcement == "evidence" or enforcement == "evidence_only"
  local reject = enforcement == "reject"
  local enforce = enforcement == "block"
  local strike_count = nil

  if enforcement == "strike" then
    local strike_subject = key_fp or remote_addr
    local strike_key = "strike:" .. strike_subject .. ":" .. rule_id
    local dict = ngx.shared.ai_risk_guard_v8
    if dict then
      local new_count, err = dict:incr(strike_key, 1, 0, STRIKE_TTL_SECONDS)
      if not new_count then
        log_error("strike incr failed: " .. tostring(err))
        new_count = 1
      end
      strike_count = new_count
      enforce = new_count >= STRIKE_THRESHOLD
    end
  end

  if enforce then
    dict_set("ip:" .. remote_addr, risk_case_id, block_ttl)
    if key_fp then
      dict_set("key:" .. key_fp, risk_case_id, block_ttl)
    end
  end

  write_event({
    ts = ngx.utctime(),
    risk_case_id = risk_case_id,
    severity = rule.severity or "high",
    action = observe and "evidence_only" or (reject and "reject" or (enforce and "block" or "strike")),
    enforce = enforce,
    strike_count = strike_count,
    strike_threshold = enforcement == "strike" and STRIKE_THRESHOLD or nil,
    cached = false,
    remote_addr = remote_addr,
    api_key_hash = key_fp,
    api_key_suffix = token_suffix,
    method = ngx.req.get_method(),
    uri = ngx.var.request_uri,
    host = ngx.var.host,
    xff_raw = header_value("x-forwarded-for") or header_value("X-Forwarded-For"),
    user_agent = header_value("user-agent") or header_value("User-Agent"),
    content_type = ngx.var.content_type,
    model = model,
    body_scan_limit_bytes = BODY_SCAN_LIMIT,
    body_truncated = truncated,
    scan_scope = scan_meta and scan_meta.scan_scope,
    scan_text_length = #scan_text,
    scan_text_fields = scan_meta and scan_meta.scan_text_fields,
    rule_id = rule_id,
    rule_description = rule.description,
    rule_tags = rule.tags,
    matched_signals = matched_signals,
    risk_score = risk_score,
    score_threshold = rule.score_threshold,
    matched_pattern = matched_pattern,
    matched_excerpt = excerpt(scan_text, match_from, match_to),
    request_id = ngx.var.request_id,
  })

  if observe then
    return nil
  end

  return {
    block = true,
    risk_case_id = risk_case_id,
    cached = false,
    subject = "rule",
    enforce = enforce,
  }
end

local ok, decision = xpcall(evaluate, debug.traceback)
if not ok then
  log_error("fail-open runtime error: " .. tostring(decision))
  return
end

if decision and decision.block then
  return response_json(ngx.HTTP_FORBIDDEN, {
    error = "request blocked by ai risk guard",
    risk_case_id = decision.risk_case_id,
  })
end
