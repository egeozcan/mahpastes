-- Tag Events Test Plugin for mahpastes
-- Tests tag events and Tags API for e2e verification

Plugin = {
    name = "Tag Events Test",
    version = "1.0.0",
    description = "Test plugin for verifying tag events and Tags API",
    author = "test",

    -- No network access needed
    network = {},

    -- No filesystem access needed
    filesystem = {
        read = false,
        write = false,
    },

    -- Subscribe to all tag events
    events = {
        "app:startup",
        "tag:created",
        "tag:updated",
        "tag:deleted",
        "tag:added_to_clip",
        "tag:removed_from_clip",
    },

    -- No scheduled tasks
    schedules = {},
}

-- Helper to increment a counter in storage
local function increment_counter(key)
    local count = storage.get(key) or "0"
    storage.set(key, tostring(tonumber(count) + 1))
end

-- Called when the app starts
function on_startup()
    log("Tag Events Test plugin started!")

    -- Test tags.list()
    local all_tags = tags.list()
    storage.set("api_list_works", tostring(#all_tags >= 0))
    log("tags.list() returned " .. #all_tags .. " tags")

    -- Test tags.create()
    local new_tag, err = tags.create("test-api-tag-" .. os.time())
    if new_tag then
        storage.set("api_create_works", "true")
        storage.set("created_tag_id", tostring(new_tag.id))
        log("Created tag: " .. new_tag.name .. " (ID: " .. new_tag.id .. ")")

        -- Test tags.get()
        local fetched = tags.get(new_tag.id)
        storage.set("api_get_works", tostring(fetched ~= nil))
        if fetched then
            log("Fetched tag: " .. fetched.name)
        end

        -- Test tags.update()
        local updated = tags.update(new_tag.id, {name = "updated-" .. os.time()})
        storage.set("api_update_works", tostring(updated == true))
        log("Updated tag: " .. tostring(updated))

        -- Test tags.delete()
        local deleted = tags.delete(new_tag.id)
        storage.set("api_delete_works", tostring(deleted == true))
        log("Deleted tag: " .. tostring(deleted))
    else
        storage.set("api_create_works", "false")
        log("Failed to create tag: " .. tostring(err))
    end

    storage.set("startup_complete", "true")
end

-- Called when a new tag is created
function on_tag_created(tag)
    log("Tag created: " .. tag.name .. " (ID: " .. tag.id .. ", color: " .. tag.color .. ")")
    storage.set("last_event", "tag:created")
    storage.set("last_tag_name", tag.name)
    storage.set("last_tag_id", tostring(tag.id))
    storage.set("last_tag_color", tag.color)
    increment_counter("tag_created_count")
end

-- Called when a tag is updated
function on_tag_updated(tag)
    log("Tag updated: " .. tag.name .. " (ID: " .. tag.id .. ", color: " .. tag.color .. ")")
    storage.set("last_event", "tag:updated")
    storage.set("last_tag_name", tag.name)
    storage.set("last_tag_id", tostring(tag.id))
    storage.set("last_tag_color", tag.color)
    increment_counter("tag_updated_count")
end

-- Called when a tag is deleted
function on_tag_deleted(tag_id)
    log("Tag deleted: ID " .. tag_id)
    storage.set("last_event", "tag:deleted")
    storage.set("last_deleted_tag_id", tostring(tag_id))
    increment_counter("tag_deleted_count")
end

-- Called when a tag is added to a clip
function on_tag_added_to_clip(data)
    log("Tag " .. data.tag_id .. " added to clip " .. data.clip_id)
    storage.set("last_event", "tag:added_to_clip")
    storage.set("last_clip_id", tostring(data.clip_id))
    storage.set("last_tag_id", tostring(data.tag_id))
    increment_counter("tag_added_count")
end

-- Called when a tag is removed from a clip
function on_tag_removed_from_clip(data)
    log("Tag " .. data.tag_id .. " removed from clip " .. data.clip_id)
    storage.set("last_event", "tag:removed_from_clip")
    storage.set("last_clip_id", tostring(data.clip_id))
    storage.set("last_tag_id", tostring(data.tag_id))
    increment_counter("tag_removed_count")
end
