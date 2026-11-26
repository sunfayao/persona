"""Mock deletion of iMessage data held by the hub."""


def register(d):
    def delete_data(h, req):
        if d.hub.IMESSAGE_STATUS.get("ai"):
            return h.send_json({"error": "先关掉「交给 Persona」再删除", "code": "ai_on"}, 409)
        return h.send_json({"chats": 6, "people": 9, "inbox": 1})

    d.route("DELETE", r"/api/v1/imessage/data", delete_data)
