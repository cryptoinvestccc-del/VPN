import { useResource } from '../lib/api'
import { fallbackLocations, fallbackPlans, fallbackStatus } from '../lib/fallback'
import type { Location, Plan, Status } from '../lib/types'

import { Header } from './Header'
import { Hero } from './Hero'
import { HowItWorks } from './HowItWorks'
import { Included } from './Included'
import { Locations } from './Locations'
import { Pricing } from './Pricing'
import { Honest } from './Honest'
import { Footer } from './Footer'

export function Landing() {
  const status = useResource<Status>('/api/v1/status', fallbackStatus)
  const locations = useResource<Location[]>('/api/v1/locations', fallbackLocations)
  const plans = useResource<Plan[]>('/api/v1/plans', fallbackPlans)

  return (
    <>
      <Header />
      <main id="main">
        <Hero status={status} />
        <HowItWorks />
        <Included />
        <Locations locations={locations} sample={status.data.mock} />
        <Pricing plans={plans} />
        <Honest />
      </main>
      <Footer />
    </>
  )
}
