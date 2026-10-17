document.addEventListener("DOMContentLoaded", () => {
    const messageForm = document.getElementById("messageForm")
    const messagesList = document.getElementById("messagesList")
    const loadMoreButton = document.getElementById("loadMoreMessages")
    const pageSize = 20
    let currentPage = 1
    let loading = false

    loadMessages()
    loadMoreButton.addEventListener("click", () => loadMessages(true))

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
            .catch((error) => {
                const message = error.message === "please wait before submitting another message"
                    ? "Please wait one minute before posting again."
                    : error.message === "message submission is temporarily unavailable"
                        ? "Message submission is temporarily unavailable. Please try again shortly."
                        : "Failed to send message. Please check the fields and try again."
                showNotification(message, "error")
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

    async function loadMessages(append = false) {
        if (loading) return
        loading = true
        loadMoreButton.disabled = true
        const requestedPage = append ? currentPage + 1 : 1
        if (append) {
            loadMoreButton.textContent = "Loading..."
        } else {
            loadMoreButton.hidden = true
            messagesList.innerHTML = '<div class="message-loading">Loading messages...</div>'
        }

        try {
            const result = await fetchMessages(requestedPage)
            const messages = result.messages.map(createMessageElement)
            if (append) {
                messagesList.querySelectorAll(".message-error").forEach((error) => error.remove())
                messages.forEach((message) => messagesList.appendChild(message))
            } else {
                messagesList.replaceChildren(...messages)
            }
            currentPage = requestedPage
            loadMoreButton.hidden = currentPage * result.pageSize >= result.total
            if (!append && !messages.length) {
                messagesList.innerHTML = '<div class="no-messages">No messages yet. Be the first to leave a message!</div>'
            }
        } catch {
            const error = document.createElement("div")
            error.className = "message-error"
            error.textContent = "Failed to load messages. Please try again."
            if (append) {
                const retry = document.createElement("button")
                retry.className = "retry-messages"
                retry.type = "button"
                retry.textContent = "Try again"
                retry.addEventListener("click", () => loadMessages(true))
                error.appendChild(retry)
                messagesList.appendChild(error)
            } else {
                messagesList.replaceChildren(error)
            }
        } finally {
            loading = false
            loadMoreButton.disabled = false
            loadMoreButton.textContent = "Load older messages"
        }
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

    function fetchMessages(page) {
        return API.get(`/messages?page=${page}&page_size=${pageSize}`)
            .then((resp) => {
                if (!Array.isArray(resp.data) || typeof resp.total !== "number") {
                    throw new Error("Invalid message response")
                }
                return {
                    messages: resp.data.map((item) => ({
                        id: item.id,
                        name: item.name || "Anonymous",
                        email: item.email || "",
                        content: item.content || "",
                        ip: item.ip || "",
                        date: item.create_at || null,
                    })),
                    total: resp.total,
                    pageSize: resp.page_size,
                }
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
