import { z } from 'zod'
import { requestJson } from '@/shared/api/http'

const healthSchema = z.object({
  status: z.literal('ok'),
  service: z.string(),
  version: z.string(),
  env: z.string(),
})

export type Health = z.infer<typeof healthSchema>

export async function getHealth(): Promise<Health> {
  const response = await requestJson<unknown>('/healthz')
  return healthSchema.parse(response)
}
