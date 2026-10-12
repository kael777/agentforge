import { Link } from 'react-router-dom'

export function NotFoundPage() {
  return (
    <section className="content-card not-found">
      <p className="eyebrow">404</p>
      <h1>这个页面还不存在</h1>
      <p>检查地址，或者回到脚手架首页继续。</p>
      <Link className="button button--primary" to="/">
        返回首页
      </Link>
    </section>
  )
}
