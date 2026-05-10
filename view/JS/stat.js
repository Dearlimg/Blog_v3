document.addEventListener("DOMContentLoaded", () => {
    const totalPV = document.getElementById('totalPV')
    const totalUV = document.getElementById('totalUV')
    const todayPV = document.getElementById('todayPV')
    const todayUV = document.getElementById('todayUV')
    const historyBody = document.getElementById('historyBody')
    const toggleHistory = document.getElementById('toggleHistory')
    const historyTable = document.getElementById('historyTable')

    if (toggleHistory && historyTable) {
        toggleHistory.addEventListener('click', () => {
            const visible = historyTable.style.display !== 'none'
            historyTable.style.display = visible ? 'none' : 'block'
        })
    }

    loadStats()

    async function loadStats() {
        try {
            const resp = await API.get('/stats')
            if (!resp.data) return

            const d = resp.data
            if (totalPV) totalPV.textContent = d.total.pv.toLocaleString()
            if (totalUV) totalUV.textContent = d.total.uv.toLocaleString()
            if (todayPV) todayPV.textContent = d.today.pv.toLocaleString()
            if (todayUV) todayUV.textContent = d.today.uv.toLocaleString()

            if (historyBody && d.history && d.history.length > 0) {
                historyBody.innerHTML = d.history.map(r =>
                    `<tr><td>${r.date}</td><td>${r.pv.toLocaleString()}</td><td>${r.uv.toLocaleString()}</td></tr>`
                ).join('')
            }
        } catch (e) {
            console.error('load stats failed', e)
        }
    }
})
