import { NavLink, Outlet } from 'react-router-dom'

const navClassName = ({ isActive }: { isActive: boolean }) =>
  isActive ? 'nav-link nav-link--active' : 'nav-link'

export function AppLayout() {
  return (
    <div className="app-shell">
      <header className="site-header">
        <NavLink className="brand" to="/" aria-label="返回首页">
          <span className="brand-mark">A</span>
          <span>AgentForge Web</span>
        </NavLink>
        <nav className="site-nav" aria-label="主导航">
          <NavLink className={navClassName} to="/" end>
            首页
          </NavLink>
          <NavLink className={navClassName} to="/about">
            关于脚手架
          </NavLink>
        </nav>
      </header>

      <main className="page">
        <Outlet />
      </main>

      <footer className="site-footer">
        React + Vite 通用前端基线 · 后台管理端可在 frontend/admin 独立建设
      </footer>
    </div>
  )
}
