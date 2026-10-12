import { z } from 'zod'

const envSchema = z.object({
  VITE_API_BASE_URL: z.string().trim().default(''),
})

const parsedEnv = envSchema.parse(import.meta.env)

export const env = {
  apiBaseUrl: parsedEnv.VITE_API_BASE_URL.replace(/\/$/, ''),
}
