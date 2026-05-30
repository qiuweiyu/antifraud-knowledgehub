import { defineStore } from 'pinia'

export const useAppStore = defineStore('app', {
  state: () => ({ apiOnline: false }),
  actions: {
    setApiOnline(value: boolean) {
      this.apiOnline = value
    }
  }
})
