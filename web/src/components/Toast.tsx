import { useToastStore } from '@/store/toast'

export function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts)

  return (
    <div className="fixed top-4 right-4 z-50 space-y-2">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className="animate-fade-in rounded-lg px-4 py-3 shadow-lg text-sm font-medium max-w-sm"
          style={{
            backgroundColor:
              toast.type === 'success' ? 'rgba(5, 150, 105, 0.9)' :
              toast.type === 'error' ? 'rgba(220, 38, 38, 0.9)' :
              toast.type === 'warning' ? 'rgba(245, 158, 11, 0.9)' :
              'rgba(51, 65, 85, 0.9)',
            color: '#fff',
          }}
        >
          {toast.message}
        </div>
      ))}
    </div>
  )
}
