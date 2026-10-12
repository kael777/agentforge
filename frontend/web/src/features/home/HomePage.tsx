import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { getHealth } from '@/shared/api/health'

const features = [
  {
    title: '清晰分层',
    description: '按 app、features、pages、shared 组织代码，业务增长时仍容易定位。',
  },
  {
    title: '数据边界',
    description: '用 TanStack Query 管理服务端状态，用 Zod 校验接口与环境配置。',
  },
  {
    title: '可验证',
    description: '预置类型检查、代码检查和组件测试，提交前可一条命令完成校验。',
  },
]

export function HomePage() {
  const health = useQuery({
    queryKey: ['health'],
    queryFn: getHealth,
  })

  return (
    <>
      <section className="hero-section">
        <p className="eyebrow">REACT APPLICATION STARTER</p>
        <h1>一个可继续生长的 React 前端基线</h1>
        <p className="hero-copy">
          面向普通前台或中台项目，先建立路由、请求、配置、测试和工程边界；具体业务能力按 feature 逐步加入。
        </p>
        <div className="hero-actions">
          <a className="button button--primary" href="#foundation">
            查看工程能力
          </a>
          <Link className="button button--quiet" to="/about">
            了解目录设计
          </Link>
        </div>

        <div className="health-card" aria-live="polite">
          <span
            className={`status-dot status-dot--${
              health.isSuccess ? 'ok' : health.isError ? 'error' : 'pending'
            }`}
          />
          {health.isPending && '正在连接 Go 后端…'}
          {health.isError && '暂未连接后端，请先启动 55555 端口的 API 服务'}
          {health.isSuccess &&
            `后端已连接：${health.data.service} · ${health.data.env} · ${health.data.version}`}
        </div>
      </section>

      <section className="foundation-section" id="foundation">
        <div className="section-heading">
          <p className="eyebrow">FOUNDATION</p>
          <h2>脚手架只提供稳定的起点</h2>
          <p>它不是管理后台成品，也不会替业务提前决定菜单、权限模型或 UI 组件库。</p>
        </div>
        <div className="feature-grid">
          {features.map((feature, index) => (
            <article className="feature-card" key={feature.title}>
              <span className="feature-number">0{index + 1}</span>
              <h3>{feature.title}</h3>
              <p>{feature.description}</p>
            </article>
          ))}
        </div>
      </section>
    </>
  )
}
