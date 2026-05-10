document.addEventListener("DOMContentLoaded", () => {
    const commentsList = document.getElementById('commentsList')

    loadComments()

    async function loadComments() {
        if (!commentsList) return

        commentsList.innerHTML = '<div class="message-loading">Loading comments...</div>'

        try {
            const response = await API.get('/comments?page=1&page_size=50')

            if (!response.data || !Array.isArray(response.data) || response.data.length === 0) {
                commentsList.innerHTML = '<div class="no-comments">No comments yet.</div>'
                return
            }

            commentsList.innerHTML = ''
            response.data.forEach(comment => {
                const commentElement = createCommentElement(comment)
                commentsList.appendChild(commentElement)
            })
        } catch (error) {
            console.error('Error loading comments:', error)
            commentsList.innerHTML = '<div class="message-error">Failed to load comments. Please refresh the page.</div>'
        }
    }

    function createCommentElement(comment) {
        const commentDiv = document.createElement('div')
        commentDiv.className = 'comment-item'
        commentDiv.dataset.commentId = comment.id

        const formattedDate = formatDate(comment.created_at)

        commentDiv.innerHTML = `
            <div class="comment-header">
                <div class="comment-author">
                    <i class="fas fa-user"></i>
                    ${escapeHTML(comment.username || 'Anonymous')}
                </div>
                <div class="comment-date">${formattedDate}</div>
            </div>
            <div class="comment-content">${escapeHTML(comment.content)}</div>
            <div class="replies-container" id="replies-${comment.id}">
                ${comment.replies && comment.replies.length > 0
                    ? comment.replies.map(reply => createReplyElement(reply)).join('')
                    : ''
                }
            </div>
        `

        return commentDiv
    }

    function createReplyElement(reply) {
        const formattedDate = formatDate(reply.created_at)
        return `
            <div class="reply-item">
                <div class="reply-header">
                    <span class="reply-author">${escapeHTML(reply.username || 'Anonymous')}</span>
                    <span class="reply-date">${formattedDate}</span>
                </div>
                <div class="reply-content">${escapeHTML(reply.content)}</div>
            </div>
        `
    }

    function formatDate(dateString) {
        if (!dateString) return 'Unknown date'
        try {
            const date = new Date(dateString)
            return date.toLocaleDateString('en-US', {
                year: 'numeric',
                month: 'short',
                day: 'numeric',
                hour: '2-digit',
                minute: '2-digit'
            })
        } catch (e) {
            return String(dateString)
        }
    }

    function escapeHTML(str) {
        if (!str) return ''
        const div = document.createElement('div')
        div.textContent = str
        return div.innerHTML
    }
})
