document.addEventListener("DOMContentLoaded", () => {
    const messageForm = document.getElementById("messageForm")
    const messagesList = document.getElementById("messagesList")

    // Load existing messages when page loads
    loadMessages()

    // Handle form submission
    messageForm.addEventListener("submit", (e) => {
        e.preventDefault()

        // Get form data
        const formData = new FormData(messageForm)
        const messageData = {
            name: formData.get("name"),
            email: formData.get("email"),
            content: formData.get("message"), // Changed to match backend field name
        }

        // Show loading state
        const submitBtn = messageForm.querySelector(".submit-btn")
        const originalBtnText = submitBtn.innerHTML
        submitBtn.innerHTML = '<i class="fas fa-spinner fa-spin"></i> Sending...'
        submitBtn.disabled = true

        // Call your backend API to save the message
        saveMessage(messageData)
            .then((response) => {
                // Show success message
                showNotification("Message sent successfully!", "success")

                // Reset form
                messageForm.reset()

                // Reload messages to show the new one
                loadMessages()
            })
            .catch((error) => {
                // Show error message
                showNotification("Failed to send message. Please try again.", "error")
                console.error("Error saving message:", error)
            })
            .finally(() => {
                // Reset button state
                submitBtn.innerHTML = originalBtnText
                submitBtn.disabled = false
            })
    })

    // Function to show notification
    function showNotification(message, type) {
        // Remove any existing notifications
        const existingNotifications = document.querySelectorAll(".message-success, .message-error")
        existingNotifications.forEach((notification) => notification.remove())

        // Create notification element
        const notification = document.createElement("div")
        notification.className = `message-${type}`
        notification.textContent = message

        // Insert notification before the form
        messageForm.parentNode.insertBefore(notification, messageForm)

        // Auto-remove notification after 5 seconds
        setTimeout(() => {
            notification.remove()
        }, 5000)
    }

    // Function to load messages from your backend
    function loadMessages() {
        messagesList.innerHTML = '<div class="message-loading">Loading messages...</div>'

        // Call your backend API to get messages
        fetchMessages()
            .then((messages) => {
                console.log("Messages to render:", messages) // Debug log

                if (!messages || messages.length === 0) {
                    messagesList.innerHTML = '<div class="no-messages">No messages yet. Be the first to leave a message!</div>'
                    return
                }

                // Clear loading message
                messagesList.innerHTML = ""

                // Add each message to the list
                messages.forEach((message) => {
                    console.log("Processing message:", message) // Debug log for each message
                    const messageElement = createMessageElement(message)
                    messagesList.appendChild(messageElement)
                })
            })
            .catch((error) => {
                messagesList.innerHTML = '<div class="message-error">Failed to load messages. Please refresh the page.</div>'
                console.error("Error loading messages:", error)
            })
    }

    // Function to create a message element
    function createMessageElement(message) {
        console.log("Creating element for message:", message) // Debug log

        const messageElement = document.createElement("div")
        messageElement.className = "message-item"

        // Ensure message has all required properties
        if (!message) {
            console.error("Invalid message object:", message)
            return messageElement
        }

        // Format date safely
        let formattedDate = "Unknown date"
        try {
            if (message.date) {
                formattedDate = formatDate(new Date(message.date))
            }
        } catch (e) {
            console.error("Date formatting error:", e)
            formattedDate = String(message.date || "Unknown date")
        }

        // Build message HTML with fallbacks for missing data
        messageElement.innerHTML = `
      <div class="message-header">
        <div class="message-author">${escapeHTML(message.name || "Anonymous")}</div>
        <div class="message-date">${formattedDate}</div>
      </div>
      <div class="message-content">${escapeHTML(message.message || message.content || "No content")}</div>
      ${message.email ? `<div class="message-email">Email: ${escapeHTML(message.email)}</div>` : ""}
    `

        return messageElement
    }

    // Function to format date
    function formatDate(date) {
        // Check if date is valid
        if (isNaN(date.getTime())) {
            return "Invalid date"
        }

        const options = {
            year: "numeric",
            month: "short",
            day: "numeric",
            hour: "2-digit",
            minute: "2-digit",
        }
        return date.toLocaleDateString("en-US", options)
    }

    // Function to escape HTML to prevent XSS
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

    // Function to fetch messages from your backend
    function fetchMessages() {
        return API.get('/messages')
            .then((response) => {
                console.log("Raw API data:", response)

                if (!response.data || !Array.isArray(response.data)) {
                    return []
                }

                // Map backend data to frontend format
                const mappedMessages = response.data
                    .map((item) => {
                        if (!item) return null

                        return {
                            id: item.id,
                            name: item.name || "Anonymous",
                            email: item.email || "",
                            message: item.content || "",
                            date: item.create_at || new Date().toISOString(),
                        }
                    })
                    .filter((item) => item !== null)

                console.log("Mapped messages:", mappedMessages)
                return mappedMessages
            })
            .catch((error) => {
                console.error("Fetch error:", error)
                throw error
            })
    }

    // Function to save a new message
    function saveMessage(messageData) {
        console.log("Sending message data:", messageData)
        return API.post('/messages', messageData)
            .then((response) => {
                console.log("Save message response:", response)
                return response
            })
            .catch((error) => {
                console.error("Error saving message:", error)
                throw error
            })
    }
})
