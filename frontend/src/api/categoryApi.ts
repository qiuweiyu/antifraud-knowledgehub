import { client, unwrap } from './client'
import type { Category } from '@/types'

export const categoryApi = {
  list: () => unwrap<Category[]>(client.get('/categories'))
}
