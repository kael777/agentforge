import { z } from 'zod'
import { requestJson } from '@/shared/api/http'

// 公网 /healthz 只承诺"进程能应答"，不包含版本、环境或提交信息。
// 前端因此只校验这一个字段，不要把内部元数据重新引入 bundle。
const healthSchema = z.object({
  status: z.literal('ok'),
})

export type Health = z.infer<typeof healthSchema>

export async function getHealth(): Promise<Health> {
  const response = await requestJson<unknown>('/healthz')
  return healthSchema.parse(response)
}
