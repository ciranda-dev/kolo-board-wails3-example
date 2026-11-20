import React from 'react'
import ReactDOM from 'react-dom/client'
import { Notch } from './Notch.jsx'
import { CrashBoundary } from './Crash.jsx'
import { installCrashHandlers } from './crash.js'
import { housingOf, showHousing } from './housing.js'

showHousing(document.documentElement, housingOf(window.location.search))
installCrashHandlers()
ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <CrashBoundary>
      <Notch />
    </CrashBoundary>
  </React.StrictMode>,
)
