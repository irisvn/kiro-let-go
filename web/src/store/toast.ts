import { create } from 'zustand'

interface Toast {
  id: number
  message: string
  type: 'success' | 'error' | 'warning' | 'info'
}

interface ToastState {
  toasts: Toast[]
  nextId: number
  addToast: (message: string, type?: Toast['type']) => void
  removeToast: (id: number) => void
}

export const useToastStore = create<ToastState>((set) => ({
  toasts: [],
  nextId: 1,

  addToast: (message, type = 'info') => {
    const id = Date.now() + Math.random()
    set((s) => ({
      toasts: [...s.toasts, { id, message, type }],
      nextId: s.nextId + 1,
    }))
    setTimeout(() => {
      set((s) => ({ toasts: s.toasts.filter(t => t.id !== id) }))
    }, 4000)
  },

  removeToast: (id) => {
    set((s) => ({ toasts: s.toasts.filter(t => t.id !== id) }))
  },
}))
