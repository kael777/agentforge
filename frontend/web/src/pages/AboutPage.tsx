export function AboutPage() {
  return (
    <section className="content-card">
      <p className="eyebrow">ABOUT</p>
      <h1>先做通用工程能力，再长出具体产品</h1>
      <p>
        当前 web 项目适合承载官网、用户端、业务工作台或中台页面。它已经准备好路由、API
        请求、服务端状态和测试，但没有绑定管理后台常见的侧栏、RBAC 和表格组件。
      </p>
      <p>
        如果以后需要独立管理端，可以在 <code>frontend/admin</code>
        建立另一个应用，共享接口契约，但保持发布节奏和用户体验相互独立。
      </p>
    </section>
  )
}
