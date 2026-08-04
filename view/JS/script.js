document.addEventListener("DOMContentLoaded", () => {
    // Animate the orbit dots
    animateOrbitDots()

    // Add hover effects to navigation
    const navItems = document.querySelectorAll(".nav-item")
    navItems.forEach((item) => {
        item.addEventListener("mouseenter", function () {
            this.style.transform = "translateY(-3px)"
        })

        item.addEventListener("mouseleave", function () {
            this.style.transform = "translateY(0)"
        })
    })

    // Add scroll reveal animation
    const sections = document.querySelectorAll("section")
    const observer = new IntersectionObserver(
        (entries) => {
            entries.forEach((entry) => {
                if (entry.isIntersecting) {
                    entry.target.classList.add("visible")
                }
            })
        },
        { threshold: 0.1 },
    )

    sections.forEach((section) => {
        section.style.opacity = "0"
        section.style.transform = "translateY(20px)"
        section.style.transition = "opacity 0.6s ease, transform 0.6s ease"
        observer.observe(section)
    })

    // Add the visible class
    document.head.insertAdjacentHTML(
        "beforeend",
        `
        <style>
            section.visible {
                opacity: 1 !important;
                transform: translateY(0) !important;
            }
        </style>
    `,
    )
})

function animateOrbitDots() {
    // Each dot has its own starting angle, rotation speed, and floating params.
    // floatSpeed / floatPhase drive a vertical sine offset so dots bob up and down
    // while they travel around the orbit.
    const dots = [
        { el: document.querySelector(".google-dot"), startAngle: 0.3, speed: 0.000275, floatSpeed: 0.00035, floatAmp: 14, floatPhase: 0 },
        { el: document.querySelector(".microsoft-dot"), startAngle: 1.9, speed: 0.000475, floatSpeed: 0.0005, floatAmp: 10, floatPhase: 2.1 },
        { el: document.querySelector(".bytedance-dot"), startAngle: 3.5, speed: 0.000725, floatSpeed: 0.0007, floatAmp: 18, floatPhase: 4.2 },
        { el: document.querySelector(".ant-dot"), startAngle: 4.9, speed: 0.00035, floatSpeed: 0.000425, floatAmp: 12, floatPhase: 1.1 },
        { el: document.querySelector(".tencent-dot"), startAngle: 0.9, speed: 0.0006, floatSpeed: 0.00085, floatAmp: 8, floatPhase: 3.3 },
    ].filter((d) => d.el)

    const container = document.querySelector(".profile-container")

    // A dot freezes in place while the mouse is hovering over it
    const hoveredDots = new Set()
    dots.forEach((dot) => {
        dot.el.addEventListener("mouseenter", () => hoveredDots.add(dot))
        dot.el.addEventListener("mouseleave", () => hoveredDots.delete(dot))
    })

    // Geometry is re-measured on resize
    let centerX = 0
    let centerY = 0
    let radius = 0
    function measure() {
        const rect = container.getBoundingClientRect()
        centerX = rect.width / 2
        centerY = rect.height / 2
        radius = Math.min(rect.width, rect.height) * 0.6
    }
    measure()
    window.addEventListener("resize", measure)

    // Timestamp-driven so movement speed stays consistent at any refresh rate
    let lastTime = null
    function updatePositions(timestamp) {
        if (lastTime === null) lastTime = timestamp
        const delta = timestamp - lastTime
        lastTime = timestamp

        dots.forEach((dot) => {
            // Advance orbit angle and floating phase only while not hovered
            if (!hoveredDots.has(dot)) {
                dot.angle = (dot.angle ?? dot.startAngle) + dot.speed * delta
                dot.floatPhase = (dot.floatPhase ?? 0) + dot.floatSpeed * delta
            }

            const x = centerX + radius * Math.cos(dot.angle)
            // Vertical sine offset gives the bobbing / floating feel
            const y = centerY + radius * Math.sin(dot.angle) + dot.floatAmp * Math.sin(dot.floatPhase)

            dot.el.style.left = `${x}px`
            dot.el.style.top = `${y}px`
            dot.el.style.transform = "translate(-50%, -50%)"
        })

        requestAnimationFrame(updatePositions)
    }

    // Start the animation
    requestAnimationFrame(updatePositions)
}
