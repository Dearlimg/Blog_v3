document.addEventListener("DOMContentLoaded", () => {
    const messageForm = document.getElementById("messageForm")
    const messagesList = document.getElementById("messagesList")

    loadMessages()

    messageForm.addEventListener("submit", (e) => {
        e.preventDefault()

        const formData = new FormData(messageForm)
        const messageData = {
            name: formData.get("name") || "",
            email: formData.get("email"),
            content: formData.get("message"),
        }

        const submitBtn = messageForm.querySelector(".submit-btn")
        const originalBtnText = submitBtn.innerHTML
        submitBtn.innerHTML = '<i class="fas fa-spinner fa-spin"></i> Sending...'
        submitBtn.disabled = true

        saveMessage(messageData)
            .then(() => {
                showNotification("Message sent successfully!", "success")
                messageForm.reset()
                loadMessages()
            })
            .catch(() => {
                showNotification("Failed to send message. Please try again.", "error")
            })
            .finally(() => {
                submitBtn.innerHTML = originalBtnText
                submitBtn.disabled = false
            })
    })

    function showNotification(message, type) {
        const existing = document.querySelectorAll(".message-success, .message-error")
        existing.forEach((n) => n.remove())

        const notification = document.createElement("div")
        notification.className = `message-${type}`
        notification.textContent = message
        messageForm.parentNode.insertBefore(notification, messageForm)
        setTimeout(() => notification.remove(), 5000)
    }

    function loadMessages() {
        messagesList.innerHTML = '<div class="message-loading">Loading messages...</div>'

        fetchMessages()
            .then((messages) => {
                if (!messages || messages.length === 0) {
                    messagesList.innerHTML = '<div class="no-messages">No messages yet. Be the first to leave a message!</div>'
                    return
                }
                messagesList.innerHTML = ""
                messages.forEach((msg) => {
                    messagesList.appendChild(createMessageElement(msg))
                })
            })
            .catch(() => {
                messagesList.innerHTML = '<div class="message-error">Failed to load messages. Please refresh the page.</div>'
            })
    }

    function createMessageElement(message) {
        const el = document.createElement("div")
        el.className = "message-item"

        const formattedDate = formatDate(message.date || message.create_at)

        el.innerHTML = `
            <div class="message-header">
                <div class="message-author">${escapeHTML(message.name || "Anonymous")}</div>
                <div class="message-date">${formattedDate}</div>
            </div>
            <div class="message-content">${escapeHTML(message.content || message.message || "")}</div>
            <div class="message-meta">
                ${message.email ? `<span class="meta-email">${escapeHTML(message.email)}</span>` : ""}
                ${message.ip ? `<span class="meta-ip">IP ${escapeHTML(message.ip)}</span>` : ""}
            </div>
        `
        return el
    }

    function formatDate(date) {
        if (!date) return "Unknown date"
        try {
            const d = new Date(date)
            if (isNaN(d.getTime())) return String(date)
            return d.toLocaleDateString("en-US", {
                year: "numeric",
                month: "short",
                day: "numeric",
                hour: "2-digit",
                minute: "2-digit",
            })
        } catch (e) {
            return String(date)
        }
    }

    function escapeHTML(str) {
        if (!str) return ""
        return str
            .toString()
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;")
            .replace(/'/g, "&#039;")
    }

    function fetchMessages() {
        return API.get("/messages")
            .then((resp) => {
                if (!resp.data || !Array.isArray(resp.data)) return []
                return resp.data.map((item) => ({
                    id: item.id,
                    name: item.name || "Anonymous",
                    email: item.email || "",
                    content: item.content || "",
                    ip: item.ip || "",
                    date: item.create_at || null,
                }))
            })
            .catch((err) => {
                console.error("Fetch error:", err)
                throw err
            })
    }

    function saveMessage(messageData) {
        return API.post("/messages", messageData)
            .catch((err) => {
                console.error("Error saving message:", err)
                throw err
            })
    }
})
