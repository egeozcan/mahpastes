-- Search field test plugin
-- Exercises the `search` settings field and `search` option form fields:
-- one search setting, one global action whose options include a search field
-- (so the picker can be driven without clips), and one async action that holds
-- the sandbox so a concurrent search hits the busy path.

Plugin = {
    name = "Search Field Test",
    version = "1.0.0",
    description = "Exercises the search field type",
    author = "mahpastes",

    network = {},

    settings = {
        {key = "entity", type = "search", source = "things", label = "Entity"},
    },

    ui = {
        global_actions = {
            {
                id = "pick",
                label = "Pick Entity",
                options = {
                    {id = "entity", type = "search", source = "things", label = "Entity"},
                },
            },
            {
                id = "slow",
                label = "Slow Action",
                async = true,
            },
        },
    },
}

function on_search(source, query)
    if source ~= "things" then return {} end
    local rows = {
        {value = "1", label = "Alpha"},
        {value = "2", label = "Beta"},
        {value = "12", label = "Alphabet"},
        {value = "3", label = "Gamma"},
    }
    local out = {}
    local q = string.lower(tostring(query or ""))
    for _, r in ipairs(rows) do
        if q == "" or string.find(string.lower(r.label), q, 1, true) then
            out[#out + 1] = r
        end
    end
    return out
end

function on_ui_action(action_id, clip_ids, options)
    options = options or {}

    if action_id == "slow" then
        -- Hold the sandbox so a concurrent on_search reports busy.
        local t0 = utils.time()
        while utils.time() - t0 < 6 do end
        return {success = true}
    end

    if action_id == "pick" then
        return {
            success = true,
            modal = {
                title = "Picked",
                content = "entity=" .. tostring(options.entity or "none"),
                format = "text",
            },
        }
    end

    return {success = false, error = "Unknown action: " .. tostring(action_id)}
end

log("search field test plugin loaded")
