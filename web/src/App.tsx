import { useResource } from './lib/api'
import { fallbackLocations, fallbackPlans, fallbackStatus } from './lib/fallback'
import type { Location, Plan, Status } from './lib/types'

import { Header } from './sections/Header'
import { Hero } from './sections/Hero'
import { HowItWorks } from './sections/HowItWorks'
import { Features } from './sections/Features'
import { Modes } from './sections/Modes'
import { Locations } from './sections/Locations'
import { Platforms } from './sections/Platforms'
import { Pricing } from './sections/Pricing'
import { Limits } from './sections/Limits'
import { Faq } from './sections/Faq'
import { Closer } from './sections/Closer'
import { Footer } from './sections/Footer'

export function App() {
  const status = useResource<Status>('/api/v1/status', fallbackStatus)
  const locations = useResource<Location[]>('/api/v1/locations', fallbackLocations)
  const plans = useResource<Plan[]>('/api/v1/plans', fallbackPlans)

  return (
    <>
      <a className="skip-link" href="#main">
        К основному содержанию
      </a>
      <Header />
      <main id="main">
        <Hero status={status} />
        <HowItWorks />
        <Features />
        <Modes />
        <Locations locations={locations} sample={status.data.mock} />
        <Platforms />
        <Pricing plans={plans} />
        <Limits />
        <Faq />
        <Closer />
      </main>
      <Footer />
    </>
  )
}
