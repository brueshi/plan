-- Rex config module for plan. Not affiliated with Superlogical.
--
-- Install: copy this file next to your init.lua, then add
--
--   require("plan").setup{ key = "cmd+shift+l" }
--
-- and run `rex config reload`. Without a key, the action is still in
-- the command palette as "Toggle Plan Checklist".
--
-- Options:
--   key    key or sequence to bind, such as "cmd+j>l". Optional.
--   bin    path to the plan binary. Found on its own when left out.
--   ratio  share of the tab the existing panes keep. Default 0.65.

local M = {}

local SHELL = "com.superlogical.terminal.shell"
local TERMINAL = "com.superlogical.terminal"
local LABEL = "plan"
local DEFAULT_RATIO = 0.65

local function file_exists(path)
  local f = io.open(path, "r")
  if not f then return false end
  f:close()
  return true
end

-- Commands start with Rex's login environment, which
-- may not include where go install puts binaries.
local function find_bin()
  local home = os.getenv("HOME") or ""
  local candidates = {
    home .. "/go/bin/plan",
    home .. "/.local/bin/plan",
    "/opt/homebrew/bin/plan",
    "/usr/local/bin/plan",
  }
  local gobin = os.getenv("GOBIN")
  if gobin and gobin ~= "" then
    table.insert(candidates, 1, gobin .. "/plan")
  end
  for _, path in ipairs(candidates) do
    if file_exists(path) then return path end
  end
  return "plan"
end

local function url_decode(s)
  return (s:gsub("%%(%x%x)", function(hex)
    return string.char(tonumber(hex, 16))
  end))
end

-- The directory a shell reported (OSC 7), else the
-- foreground process's, for shells that don't report.
local function block_cwd(block_id)
  local r = rex.block.call(TERMINAL, "pwd", { block_id = block_id })
  if r and r.pwd then
    return url_decode((r.pwd:gsub("^file://[^/]*", "")))
  end
  local p = rex.block.call(TERMINAL, "process", { block_id = block_id })
  if p and p.foreground and p.foreground.cwd then
    return p.foreground.cwd
  end
  return nil
end

local function window_root(window_id)
  local view, err = rex.session.view{}
  if err ~= nil then error(err, 0) end
  for _, w in ipairs(view.windows or {}) do
    if w.window_id == window_id then
      for _, layer in ipairs(w.layers or {}) do
        if layer.kind == "tiled" then
          local root = layer.layout
          return root.split and root.split.split_id or root.block_id
        end
      end
    end
  end
  return nil
end

function M.setup(opts)
  opts = opts or {}
  local ratio = opts.ratio or DEFAULT_RATIO

  rex.action{
    name = "plan_pane",
    title = "Toggle Plan Checklist",
    description = "Show the session's active plan, or the repo's PLAN.md, beside this tab's panes.",
    category = "Agents",
    keywords = "checklist todo tasks",
    run = function(ctx)
      local listed, err = rex.session.list_blocks{}
      if err ~= nil then error(err, 0) end
      local window
      for _, b in ipairs(listed.blocks or {}) do
        if b.block_id == ctx.block_id then window = b.window_id end
      end

      -- A second press closes the pane. The plan
      -- lives in its file, so nothing is lost.
      for _, b in ipairs(listed.blocks or {}) do
        if b.window_id == window and b.label == LABEL then
          local _, cerr = rex.call("block.close", {
            session_id = ctx.session_id,
            block_id = b.block_id,
          })
          if cerr ~= nil then error(cerr, 0) end
          return { closed = true }
        end
      end

      -- Splitting the root puts the plan beside
      -- every pane in the tab, at full height.
      local result, serr = rex.session.new_split{
        anchor_id = window_root(window) or ctx.block_id,
        direction = "horizontal",
        side = "after",
        ratio = ratio,
        focus = true,
        layout = rex.layout.block{
          flavor = SHELL,
          label = LABEL,
          options = {
            cwd = block_cwd(ctx.block_id),
            command = { opts.bin or find_bin() },
          },
        },
      }
      if serr ~= nil then error(serr, 0) end
      return { block_ids = result.block_ids }
    end,
  }

  if opts.key then
    rex.bind(opts.key, "plan_pane")
  end
end

return M
